package wasm

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// PluginWatcher watches the plugins directory for new/updated plugins
// and auto-registers them in the Registry at runtime (hot-reload).
type PluginWatcher struct {
	watcher  *fsnotify.Watcher
	baseDir  string
	stopCh   chan struct{}
	doneCh   chan struct{}
	mu       sync.Mutex
	debounce map[string]*time.Timer // debounce per-dir changes
	onChange func(event string, manifest *PluginManifest) // callback
}

// NewPluginWatcher creates a filesystem watcher for the plugins directory.
// onChange is called whenever a plugin is added or updated.
func NewPluginWatcher(baseDir string, onChange func(event string, manifest *PluginManifest)) (*PluginWatcher, error) {
	absDir, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	pw := &PluginWatcher{
		watcher:  watcher,
		baseDir:  absDir,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
		debounce: make(map[string]*time.Timer),
		onChange:  onChange,
	}

	return pw, nil
}

// Start begins watching the plugins directory in a background goroutine.
// It watches for:
// - New manifest.json files (new plugin uploaded)
// - Updated manifest.json files (plugin metadata changed)
// - New/updated plugin.wasm files (plugin binary updated)
func (pw *PluginWatcher) Start() error {
	// Watch the base dir for new subdirectories
	if err := pw.watcher.Add(pw.baseDir); err != nil {
		return err
	}

	// Also watch existing plugin subdirectories
	entries, err := os.ReadDir(pw.baseDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				subDir := filepath.Join(pw.baseDir, entry.Name())
				_ = pw.watcher.Add(subDir)
			}
		}
	}

	go pw.loop()
	log.Printf("[PluginWatcher] Watching '%s' for hot-reload", pw.baseDir)
	return nil
}

// Stop gracefully shuts down the watcher.
func (pw *PluginWatcher) Stop() {
	close(pw.stopCh)
	pw.watcher.Close()
	<-pw.doneCh
	log.Printf("[PluginWatcher] Stopped")
}

func (pw *PluginWatcher) loop() {
	defer close(pw.doneCh)

	for {
		select {
		case <-pw.stopCh:
			return

		case event, ok := <-pw.watcher.Events:
			if !ok {
				return
			}
			pw.handleEvent(event)

		case err, ok := <-pw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[PluginWatcher] Error: %v", err)
		}
	}
}

func (pw *PluginWatcher) handleEvent(event fsnotify.Event) {
	name := filepath.Base(event.Name)

	// New directory created → watch it for manifest.json
	if event.Has(fsnotify.Create) {
		info, err := os.Stat(event.Name)
		if err == nil && info.IsDir() {
			_ = pw.watcher.Add(event.Name)
			log.Printf("[PluginWatcher] Now watching new directory: %s", event.Name)
			return
		}
	}

	// Only care about manifest.json and plugin.wasm changes
	if name != "manifest.json" && !strings.HasSuffix(name, ".wasm") {
		return
	}

	// Only care about Create and Write events
	if !event.Has(fsnotify.Create) && !event.Has(fsnotify.Write) {
		return
	}

	// Debounce: wait 500ms after last change in the same directory
	// (file copies often trigger multiple events)
	dir := filepath.Dir(event.Name)
	pw.mu.Lock()
	if timer, exists := pw.debounce[dir]; exists {
		timer.Stop()
	}
	pw.debounce[dir] = time.AfterFunc(500*time.Millisecond, func() {
		pw.processPluginDir(dir)
		pw.mu.Lock()
		delete(pw.debounce, dir)
		pw.mu.Unlock()
	})
	pw.mu.Unlock()
}

func (pw *PluginWatcher) processPluginDir(pluginDir string) {
	manifestPath := filepath.Join(pluginDir, "manifest.json")

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		log.Printf("[PluginWatcher] Cannot read manifest in '%s': %v", pluginDir, err)
		return
	}

	var manifest PluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		log.Printf("[PluginWatcher] Invalid manifest.json in '%s': %v", pluginDir, err)
		return
	}

	if manifest.Slug == "" {
		log.Printf("[PluginWatcher] Manifest in '%s' has no slug, skipping", pluginDir)
		return
	}

	// Set internal paths
	manifest.Dir = pluginDir
	if manifest.WasmFile != "" {
		manifest.WasmPath = filepath.Join(pluginDir, manifest.WasmFile)
	}

	// Check if this is new or updated
	existing := Registry.GetBySlug(manifest.Slug)
	eventType := "added"
	if existing != nil {
		eventType = "updated"
	}

	// Register/update in the global registry
	Registry.mu.Lock()
	Registry.entries[manifest.Slug] = &manifest
	Registry.mu.Unlock()

	log.Printf("[PluginWatcher] 🔥 Hot-reload: plugin %s → %s v%s (%s)",
		eventType, manifest.Name, manifest.Version, manifest.Slug)

	// Check if .wasm file exists
	if manifest.WasmPath != "" {
		if _, err := os.Stat(manifest.WasmPath); err == nil {
			log.Printf("[PluginWatcher] ✅ WASM binary found: %s", manifest.WasmPath)
		} else {
			log.Printf("[PluginWatcher] ⏳ WASM binary not yet available for '%s'", manifest.Slug)
		}
	}

	// Notify callback
	if pw.onChange != nil {
		pw.onChange(eventType, &manifest)
	}
}
