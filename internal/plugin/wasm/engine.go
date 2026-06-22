// Package wasm provides the runtime engine that loads and manages
// WASM plugins discovered from the plugins/ directory.
package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/saybridge/saybridge/internal/plugin"
)

// Engine manages the lifecycle of WASM plugins at runtime.
// It bridges plugin manifests with the core HookRegistry.
type Engine struct {
	mu              sync.RWMutex
	hooks           *plugin.HookRegistry
	sandbox         *Sandbox
	wasm            *WasmRuntime
	loaded          map[string]*AppAdapter // slug → adapter
	DeleteMessageFn func(ctx context.Context, roomID string, timeBucket int, messageID string) error
}

// NewEngine creates a new app runtime engine.
// If wasmRT is nil, WASM plugin loading is disabled (graceful degradation).
func NewEngine(hooks *plugin.HookRegistry, wasmRT *WasmRuntime, deleteMessageFn func(ctx context.Context, roomID string, timeBucket int, messageID string) error) *Engine {
	return &Engine{
		hooks:           hooks,
		sandbox:         NewSandbox(),
		wasm:            wasmRT,
		loaded:          make(map[string]*AppAdapter),
		DeleteMessageFn: deleteMessageFn,
	}
}

// AppAdapter wraps a plugin as a runtime entity that registers hooks.
type AppAdapter struct {
	Slug        string
	Name        string
	Version     string
	Permissions []string
	HookEvents  []string
	IsWasm      bool
}

// InstallWasmApp installs a WASM plugin by slug: compiles the .wasm file,
// instantiates the module, and registers hook handlers in the HookRegistry.
func (e *Engine) InstallWasmApp(ctx context.Context, slug string) error {
	manifest := Registry.GetBySlug(slug)
	if manifest == nil {
		return fmt.Errorf("plugin '%s' not found in registry", slug)
	}

	if e.wasm == nil {
		return fmt.Errorf("WASM runtime not initialized")
	}

	// Compile WASM module if not already compiled
	if !e.wasm.IsLoaded(slug) {
		if !Registry.HasWasm(slug) {
			log.Printf("[AppRuntime] Plugin '%s' has no .wasm file, registering hooks as stubs", slug)
			e.registerStubHooks(slug, manifest)
			return nil
		}

		if err := e.wasm.CompilePlugin(ctx, manifest); err != nil {
			return fmt.Errorf("failed to compile plugin '%s': %w", slug, err)
		}

		if err := e.wasm.InstantiatePlugin(ctx, slug); err != nil {
			return fmt.Errorf("failed to instantiate plugin '%s': %w", slug, err)
		}
	}

	// Build an adapter once for install-time permission validation.
	installAdapter := &AppAdapter{
		Slug:        slug,
		Name:        manifest.Name,
		Version:     manifest.Version,
		Permissions: manifest.Permissions,
		HookEvents:  manifest.Hooks,
		IsWasm:      true,
	}

	// Register hook handlers that dispatch to WASM on_hook
	for _, hookName := range manifest.Hooks {
		hookEvent := plugin.HookEvent(hookName)
		capturedSlug := slug
		capturedHook := hookName

		// Enforce permissions at install time: refuse to wire a hook the plugin
		// has not declared the required permissions for, rather than silently
		// no-op'ing on every invocation.
		if err := e.sandbox.CheckPermission(installAdapter, hookName); err != nil {
			log.Printf("[AppRuntime] Refusing to register hook '%s' for plugin '%s': %v", hookName, slug, err)
			continue
		}

		e.hooks.On(hookEvent, plugin.HookHandler{
			Name:     fmt.Sprintf("wasm:%s", slug),
			Priority: 50,
			Runtime:  plugin.RuntimeWASM,
			Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
				adapter := &AppAdapter{
					Slug:        capturedSlug,
					Name:        manifest.Name,
					Version:     manifest.Version,
					Permissions: manifest.Permissions,
					HookEvents:  manifest.Hooks,
					IsWasm:      true,
				}

				if err := e.sandbox.CheckPermission(adapter, capturedHook); err != nil {
					log.Printf("[AppRuntime] Warning: Permission denied for plugin '%s' on event '%s': %v", capturedSlug, capturedHook, err)
					return nil, nil
				}

				payloadBytes, _ := json.Marshal(payload)
				resVal, err := e.sandbox.ExecuteWithTimeout(ctx, manifest.Name, func(timeoutCtx context.Context) (interface{}, error) {
					return e.wasm.CallHook(timeoutCtx, capturedSlug, capturedHook, payloadBytes)
				})
				if err != nil {
					log.Printf("[AppRuntime] WASM hook error for '%s' on '%s': %v", capturedSlug, capturedHook, err)
					return nil, nil
				}

				var result int32
				if resVal != nil {
					result = resVal.(int32)
				}
				log.Printf("[AppRuntime] WASM hook '%s:%s' returned %d", capturedSlug, capturedHook, result)

				// If the message hook returned 1, soft-delete the original trigger command message
				if (capturedHook == "message.after_send" || capturedHook == "message.slash_command") && result == 1 {
					roomID, _ := payload["room_id"].(string)
					messageID, _ := payload["message_id"].(string)
					
					// time_bucket can be float64 (JSON unmarshalled) or int
					var timeBucket int
					if tbVal, ok := payload["time_bucket"]; ok {
						switch v := tbVal.(type) {
						case int:
							timeBucket = v
						case float64:
							timeBucket = int(v)
						case int32:
							timeBucket = int(v)
						case int64:
							timeBucket = int(v)
						}
					}
					
					if roomID != "" && messageID != "" && timeBucket != 0 && e.DeleteMessageFn != nil {
						log.Printf("[AppRuntime] Plugin %s successfully handled command. Soft-deleting trigger message %s (room=%s, bucket=%d)", capturedSlug, messageID, roomID, timeBucket)
						if err := e.DeleteMessageFn(ctx, roomID, timeBucket, messageID); err != nil {
							log.Printf("[AppRuntime] Failed to delete trigger message: %v", err)
						}
					}
				}

				return nil, nil
			},
		})
	}

	e.mu.Lock()
	e.loaded[slug] = &AppAdapter{
		Slug:        slug,
		Name:        manifest.Name,
		Version:     manifest.Version,
		Permissions: manifest.Permissions,
		HookEvents:  manifest.Hooks,
		IsWasm:      true,
	}
	e.mu.Unlock()

	log.Printf("[AppRuntime] Installed WASM app '%s' (v%s) with %d hooks",
		manifest.Name, manifest.Version, len(manifest.Hooks))
	return nil
}

// registerStubHooks registers placeholder hooks for plugins without .wasm files.
func (e *Engine) registerStubHooks(slug string, manifest *PluginManifest) {
	for _, hookName := range manifest.Hooks {
		hookEvent := plugin.HookEvent(hookName)
		capturedSlug := slug
		capturedHook := hookName

		e.hooks.On(hookEvent, plugin.HookHandler{
			Name:     fmt.Sprintf("stub:%s", slug),
			Priority: 50,
			Runtime:  plugin.RuntimeNative,
			Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
				log.Printf("[AppRuntime] Stub hook '%s:%s' (no .wasm file)", capturedSlug, capturedHook)
				return nil, nil
			},
		})
	}

	e.mu.Lock()
	e.loaded[slug] = &AppAdapter{
		Slug:        slug,
		Name:        manifest.Name,
		Version:     manifest.Version,
		Permissions: manifest.Permissions,
		HookEvents:  manifest.Hooks,
		IsWasm:      false,
	}
	e.mu.Unlock()

	log.Printf("[AppRuntime] Registered stub hooks for '%s' (%d hooks)", manifest.Name, len(manifest.Hooks))
}

// UninstallWasmApp unloads a WASM plugin.
// Emits plugin.before_uninstall first — if any handler returns error, uninstall is BLOCKED.
func (e *Engine) UninstallWasmApp(ctx context.Context, slug string) error {
	// Look up manifest for name
	manifest := Registry.GetBySlug(slug)
	pluginName := slug
	if manifest != nil {
		pluginName = manifest.Name
	}

	// === Fire before_uninstall hook (blocking) ===
	payload := map[string]interface{}{
		"slug": slug,
		"name": pluginName,
	}
	if err := e.hooks.Emit(ctx, plugin.BeforeUninstallPlugin, payload); err != nil {
		log.Printf("[AppRuntime] Uninstall BLOCKED for '%s': %v", slug, err)
		return fmt.Errorf("cannot uninstall '%s': %w", pluginName, err)
	}

	// === Proceed with uninstall ===
	if e.wasm != nil {
		if err := e.wasm.UnloadPlugin(ctx, slug); err != nil {
			log.Printf("[AppRuntime] Warning: failed to unload WASM for '%s': %v", slug, err)
		}
	}

	e.mu.Lock()
	delete(e.loaded, slug)
	e.mu.Unlock()

	log.Printf("[AppRuntime] Uninstalled WASM app '%s'", slug)

	// === Fire after_uninstall hook (async, non-blocking) ===
	e.hooks.EmitAsync(ctx, plugin.AfterUninstallPlugin, payload)

	return nil
}

// IsAppLoaded checks if a plugin is currently loaded.
func (e *Engine) IsAppLoaded(slug string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, exists := e.loaded[slug]
	return exists
}

// LoadedCount returns the count of loaded plugins.
func (e *Engine) LoadedCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.loaded)
}

// LoadedSlugs returns the slugs of all currently loaded plugins.
// Used by marketplace's before_uninstall hook to check for active dependents.
func (e *Engine) LoadedSlugs() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	slugs := make([]string, 0, len(e.loaded))
	for slug := range e.loaded {
		slugs = append(slugs, slug)
	}
	return slugs
}

// Close shuts down the WASM runtime.
func (e *Engine) Close() error {
	if e.wasm != nil {
		return e.wasm.Close(context.Background())
	}
	return nil
}
