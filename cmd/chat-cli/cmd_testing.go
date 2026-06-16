package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

func testCmd() *cobra.Command {
	var eventsFile string

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Build and run test events against the plugin",
		Long: `Builds the WASM plugin, loads it into a sandbox, and runs a suite
of test events. Events can be loaded from a test_events.json file
or use built-in defaults.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTests(".", eventsFile)
		},
	}

	cmd.Flags().StringVarP(&eventsFile, "events", "e", "", "Path to test events JSON file (default: built-in events)")

	return cmd
}

func simulateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "simulate <event> <payload-json>",
		Short: "Send a single test event to the plugin",
		Long: `Builds the plugin (if needed), loads it into a sandbox, and sends
a single hook event with the given payload.

Example:
  chat-cli simulate message.after_send '{"sender_id":"u1","room_id":"r1","content":"hello"}'`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSimulate(".", args[0], args[1])
		},
	}
	return cmd
}

// TestEvent represents a test case for the plugin.
type TestEvent struct {
	Name           string `json:"name"`
	Event          string `json:"event"`
	Payload        string `json:"payload"`
	ExpectedResult *int32 `json:"expected_result,omitempty"`
}

func runTests(dir, eventsFile string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	fmt.Println("╔═══════════════════════════════════════════════╗")
	fmt.Println("║     🧪 Saybridge Plugin Test Runner          ║")
	fmt.Println("╚═══════════════════════════════════════════════╝")
	fmt.Println()

	// Build
	fmt.Println("🔨 Building plugin...")
	wasmPath, err := BuildPlugin(absDir)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	fmt.Println("✅ Build successful")
	fmt.Println()

	// Load test events
	events := getTestEvents(eventsFile)

	// Run tests
	ctx := context.Background()
	passed := 0
	failed := 0

	for i, te := range events {
		fmt.Printf("─── Test %d: %s ─────────────────────────────\n", i+1, te.Name)
		fmt.Printf("  Event: %s\n", te.Event)

		sb, err := NewSandbox(ctx)
		if err != nil {
			fmt.Printf("  ❌ Sandbox error: %v\n", err)
			failed++
			continue
		}

		if err := sb.LoadPlugin(ctx, wasmPath); err != nil {
			fmt.Printf("  ❌ Load error: %v\n", err)
			sb.Close(ctx)
			failed++
			continue
		}

		start := time.Now()
		result, err := sb.CallHook(ctx, te.Event, te.Payload)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("  ❌ Execution error: %v\n", err)
			sb.Close(ctx)
			failed++
			continue
		}

		// Check expected result
		if te.ExpectedResult != nil && result != *te.ExpectedResult {
			fmt.Printf("  ❌ Expected result %d, got %d\n", *te.ExpectedResult, result)
			sb.Close(ctx)
			failed++
			continue
		}

		fmt.Printf("  ✅ Passed (result=%d, %s)\n", result, elapsed)
		fmt.Printf("     Logs: %d | Messages: %d | WS Events: %d\n",
			len(sb.GetLogs()), len(sb.GetMessages()), len(sb.GetWSEvents()))

		sb.Close(ctx)
		passed++
		fmt.Println()
	}

	// Summary
	fmt.Println("═══════════════════════════════════════════════")
	total := passed + failed
	if failed == 0 {
		fmt.Printf("✅ All %d tests passed\n", total)
	} else {
		fmt.Printf("❌ %d/%d tests failed\n", failed, total)
	}

	if failed > 0 {
		return fmt.Errorf("%d tests failed", failed)
	}
	return nil
}

func runSimulate(dir, event, payloadJSON string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	// Validate JSON
	if !json.Valid([]byte(payloadJSON)) {
		return fmt.Errorf("invalid JSON payload: %s", payloadJSON)
	}

	fmt.Printf("🎯 Simulating event: %s\n", event)
	fmt.Printf("📦 Payload: %s\n\n", payloadJSON)

	// Build
	wasmPath, err := BuildPlugin(absDir)
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	ctx := context.Background()
	sb, err := NewSandbox(ctx)
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}
	defer sb.Close(ctx)

	if err := sb.LoadPlugin(ctx, wasmPath); err != nil {
		return fmt.Errorf("load plugin: %w", err)
	}

	fmt.Println("── Execution Output ──────────────────────────")
	start := time.Now()
	result, err := sb.CallHook(ctx, event, payloadJSON)
	elapsed := time.Since(start)

	if err != nil {
		return fmt.Errorf("hook execution error: %w", err)
	}

	fmt.Println()
	fmt.Printf("── Result ────────────────────────────────────\n")
	fmt.Printf("  Return code: %d\n", result)
	fmt.Printf("  Duration:    %s\n", elapsed)
	fmt.Printf("  Logs:        %d\n", len(sb.GetLogs()))
	fmt.Printf("  Messages:    %d\n", len(sb.GetMessages()))
	fmt.Printf("  WS Events:   %d\n", len(sb.GetWSEvents()))

	return nil
}

func getTestEvents(eventsFile string) []TestEvent {
	// Try to load from file
	if eventsFile != "" {
		data, err := os.ReadFile(eventsFile)
		if err == nil {
			var events []TestEvent
			if err := json.Unmarshal(data, &events); err == nil && len(events) > 0 {
				return events
			}
		}
	}

	// Try default test_events.json in current directory
	if data, err := os.ReadFile("test_events.json"); err == nil {
		var events []TestEvent
		if err := json.Unmarshal(data, &events); err == nil && len(events) > 0 {
			return events
		}
	}

	// Built-in default test events
	zero := int32(0)
	return []TestEvent{
		{
			Name:           "Message in channel",
			Event:          "message.after_send",
			Payload:        `{"sender_id":"user-1","room_id":"general","message_id":"msg-001","content":"Hello world!","room_type":"channel","room_members_count":"5"}`,
			ExpectedResult: &zero,
		},
		{
			Name:           "Direct message",
			Event:          "message.after_send",
			Payload:        `{"sender_id":"user-1","room_id":"dm-001","message_id":"msg-002","content":"Hey, how are you?","room_type":"direct","room_members_count":"2"}`,
			ExpectedResult: &zero,
		},
		{
			Name:           "Slash command /ai",
			Event:          "message.slash_command",
			Payload:        `{"command":"ai","args":"What is Saybridge?","room_id":"general","sender_id":"user-1"}`,
			ExpectedResult: &zero,
		},
		{
			Name:           "User status change",
			Event:          "user.status_change",
			Payload:        `{"user_id":"user-1","old_status":"offline","new_status":"online"}`,
			ExpectedResult: &zero,
		},
		{
			Name:           "System message (should be skipped)",
			Event:          "message.after_send",
			Payload:        `{"sender_id":"system","room_id":"general","message_id":"msg-003","content":"User joined","room_type":"channel"}`,
			ExpectedResult: &zero,
		},
	}
}
