package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

func devCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Start hot-reload development server",
		Long: `Watches plugin source files for changes, automatically rebuilds
the WASM binary, and loads it into a sandbox for testing.

Run from the plugin directory (containing main.go and manifest.json).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevServer(".")
		},
	}
	return cmd
}

func runDevServer(dir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Verify plugin directory
	if _, err := os.Stat(filepath.Join(absDir, "main.go")); os.IsNotExist(err) {
		return fmt.Errorf("no main.go found in %s — run from a plugin directory", absDir)
	}

	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║     🔄 Saybridge Plugin Dev Server           ║")
	fmt.Println("╠═══════════════════════════════════════════════╣")
	fmt.Printf("║  Directory: %-33s ║\n", filepath.Base(absDir))
	fmt.Println("║  Watching:  *.go files                       ║")
	fmt.Println("║  Press Ctrl+C to stop                        ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Initial build
	if err := buildAndTest(absDir); err != nil {
		fmt.Printf("⚠️  Initial build failed: %v\n", err)
		fmt.Println("Watching for changes...")
	}

	// Watch for file changes
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(absDir); err != nil {
		return fmt.Errorf("watch directory: %w", err)
	}

	// Handle OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Debounce timer
	var debounceTimer *time.Timer

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			// Only react to Go file changes
			if filepath.Ext(event.Name) != ".go" {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}

			// Debounce: wait 500ms for more changes
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
				fmt.Printf("\n🔄 File changed: %s\n", filepath.Base(event.Name))
				if err := buildAndTest(absDir); err != nil {
					fmt.Printf("⚠️  Build failed: %v\n", err)
				}
				fmt.Println("\n⏳ Watching for changes...")
			})

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			log.Printf("Watcher error: %v", err)

		case <-sigCh:
			fmt.Println("\n\n👋 Dev server stopped")
			return nil
		}
	}
}

func buildAndTest(dir string) error {
	fmt.Println("🔨 Building plugin...")
	start := time.Now()

	wasmPath, err := BuildPlugin(dir)
	if err != nil {
		return err
	}

	info, _ := os.Stat(wasmPath)
	fmt.Printf("✅ Build successful (%.1fs, %d bytes)\n", time.Since(start).Seconds(), info.Size())

	// Load into sandbox and run a basic test
	fmt.Println("\n🧪 Running sanity check...")
	ctx := context.Background()

	sb, err := NewSandbox(ctx)
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}
	defer sb.Close(ctx)

	if err := sb.LoadPlugin(ctx, wasmPath); err != nil {
		return fmt.Errorf("load plugin: %w", err)
	}

	// Test with a sample message event
	testPayload := `{"sender_id":"test-user","room_id":"general","message_id":"msg-001","content":"Hello from dev server!","room_type":"channel","room_members_count":"5"}`

	fmt.Println("\n📨 Sending test event: message.after_send")
	result, err := sb.CallHook(ctx, "message.after_send", testPayload)
	if err != nil {
		fmt.Printf("⚠️  Hook execution error: %v\n", err)
	} else {
		fmt.Printf("✅ Hook returned: %d\n", result)
	}

	// Print summary
	fmt.Printf("\n📊 Summary: %d logs, %d messages, %d WS events\n",
		len(sb.GetLogs()), len(sb.GetMessages()), len(sb.GetWSEvents()))

	return nil
}
