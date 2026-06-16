package wasm

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// WasmRuntime manages wazero runtime instances for marketplace WASM plugins.
// Each plugin runs in an isolated sandbox with controlled host function access.
type WasmRuntime struct {
	mu      sync.RWMutex
	runtime wazero.Runtime
	modules map[string]*WasmModule // slug → loaded module
	hostAPI *HostAPI
}

// WasmModule represents a loaded WASM plugin module.
type WasmModule struct {
	Slug     string
	Manifest *PluginManifest
	Compiled wazero.CompiledModule
	Instance api.Module // nil until instantiated
}

// NewWasmRuntime creates a new WASM runtime with the host API.
func NewWasmRuntime(hostAPI *HostAPI) (*WasmRuntime, error) {
	ctx := context.Background()

	r := wazero.NewRuntime(ctx)

	// Instantiate WASI for system-level support (stdio, env, etc.)
	_, err := wasi_snapshot_preview1.Instantiate(ctx, r)
	if err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("failed to instantiate WASI: %w", err)
	}

	wr := &WasmRuntime{
		runtime: r,
		modules: make(map[string]*WasmModule),
		hostAPI: hostAPI,
	}

	// Register host functions module
	if err := wr.registerHostFunctions(ctx); err != nil {
		r.Close(ctx)
		return nil, fmt.Errorf("failed to register host functions: %w", err)
	}

	log.Println("[WasmRuntime] Initialized with wazero + WASI support")
	return wr, nil
}

// registerHostFunctions registers all host functions that WASM plugins can import.
func (wr *WasmRuntime) registerHostFunctions(ctx context.Context) error {
	hostModule := wr.runtime.NewHostModuleBuilder("host")

	// host.host_log(level i32, msg_ptr i32, msg_len i32)
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, level, msgPtr, msgLen uint32) {
			msg := readStringFromMemory(mod, msgPtr, msgLen)
			slug := mod.Name()
			wr.hostAPI.LogForApp(level, slug, msg)
		}).
		Export("host_log")

	// host.host_send_message(room_ptr i32, room_len i32, msg_ptr i32, msg_len i32) -> i32
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, roomPtr, roomLen, msgPtr, msgLen uint32) int32 {
			room := readStringFromMemory(mod, roomPtr, roomLen)
			msg := readStringFromMemory(mod, msgPtr, msgLen)
			slug := mod.Name()
			senderName := "System"
			if manifest := Registry.GetBySlug(slug); manifest != nil {
				senderName = manifest.Name
			}
			return wr.hostAPI.SendMessage(ctx, room, msg, senderName)
		}).
		Export("host_send_message")

	// host.host_kv_get(key_ptr i32, key_len i32) -> (val_ptr i32, val_len i32)
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, keyPtr, keyLen uint32) uint64 {
			key := readStringFromMemory(mod, keyPtr, keyLen)
			slug := mod.Name()
			val := wr.hostAPI.KVGetForApp(ctx, slug, key)
			if val == "" {
				return 0
			}
			ptr, length := writeStringToMemory(mod, val)
			return uint64(ptr)<<32 | uint64(length)
		}).
		Export("host_kv_get")

	// host.host_kv_set(key_ptr i32, key_len i32, val_ptr i32, val_len i32) -> i32
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, keyPtr, keyLen, valPtr, valLen uint32) int32 {
			key := readStringFromMemory(mod, keyPtr, keyLen)
			val := readStringFromMemory(mod, valPtr, valLen)
			slug := mod.Name()
			return wr.hostAPI.KVSetForApp(ctx, slug, key, val)
		}).
		Export("host_kv_set")

	// host.host_http_register(method_ptr i32, method_len i32, path_ptr i32, path_len i32) -> i32
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, methodPtr, methodLen, pathPtr, pathLen uint32) int32 {
			method := readStringFromMemory(mod, methodPtr, methodLen)
			path := readStringFromMemory(mod, pathPtr, pathLen)
			log.Printf("[WasmRuntime] Plugin registered HTTP route: %s %s", method, path)
			return 0
		}).
		Export("host_http_register")

	// host.host_get_room_history(room_ptr i32, room_len i32) -> (resp_ptr i32, resp_len i32)
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, roomPtr, roomLen uint32) uint64 {
			room := readStringFromMemory(mod, roomPtr, roomLen)
			val := wr.hostAPI.GetRoomHistory(ctx, room)
			if val == "" {
				return 0
			}
			ptr, length := writeStringToMemory(mod, val)
			return uint64(ptr)<<32 | uint64(length)
		}).
		Export("host_get_room_history")

	// host.host_publish_ws_event(room_ptr i32, room_len i32, event_ptr i32, event_len i32, data_ptr i32, data_len i32) -> i32
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, roomPtr, roomLen, eventPtr, eventLen, dataPtr, dataLen uint32) int32 {
			room := readStringFromMemory(mod, roomPtr, roomLen)
			event := readStringFromMemory(mod, eventPtr, eventLen)
			data := readStringFromMemory(mod, dataPtr, dataLen)
			return wr.hostAPI.PublishWSEvent(ctx, room, event, data)
		}).
		Export("host_publish_ws_event")

	// host.host_get_user_status(user_ptr i32, user_len i32) -> (resp_ptr i32, resp_len i32)
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, userPtr, userLen uint32) uint64 {
			user := readStringFromMemory(mod, userPtr, userLen)
			val := wr.hostAPI.GetUserStatus(ctx, user)
			if val == "" {
				return 0
			}
			ptr, length := writeStringToMemory(mod, val)
			return uint64(ptr)<<32 | uint64(length)
		}).
		Export("host_get_user_status")

	// host.host_delete_message(room_ptr i32, room_len i32, msg_ptr i32, msg_len i32) -> i32
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, roomPtr, roomLen, msgPtr, msgLen uint32) int32 {
			room := readStringFromMemory(mod, roomPtr, roomLen)
			msg := readStringFromMemory(mod, msgPtr, msgLen)
			return wr.hostAPI.DeleteMessage(ctx, room, msg)
		}).
		Export("host_delete_message")

	// host.host_edit_message(room_ptr i32, room_len i32, msg_ptr i32, msg_len i32, content_ptr i32, content_len i32) -> i32
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, roomPtr, roomLen, msgPtr, msgLen, contentPtr, contentLen uint32) int32 {
			room := readStringFromMemory(mod, roomPtr, roomLen)
			msg := readStringFromMemory(mod, msgPtr, msgLen)
			content := readStringFromMemory(mod, contentPtr, contentLen)
			return wr.hostAPI.EditMessage(ctx, room, msg, content)
		}).
		Export("host_edit_message")

	// host.host_create_room(name_ptr i32, name_len i32, type_ptr i32, type_len i32) -> (id_ptr i32, id_len i32)
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, namePtr, nameLen, typePtr, typeLen uint32) uint64 {
			name := readStringFromMemory(mod, namePtr, nameLen)
			rType := readStringFromMemory(mod, typePtr, typeLen)
			val := wr.hostAPI.CreateRoom(ctx, name, rType)
			if val == "" {
				return 0
			}
			ptr, length := writeStringToMemory(mod, val)
			return uint64(ptr)<<32 | uint64(length)
		}).
		Export("host_create_room")

	// host.host_get_user(user_ptr i32, user_len i32) -> (resp_ptr i32, resp_len i32)
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, userPtr, userLen uint32) uint64 {
			user := readStringFromMemory(mod, userPtr, userLen)
			val := wr.hostAPI.GetUser(ctx, user)
			if val == "" {
				return 0
			}
			ptr, length := writeStringToMemory(mod, val)
			return uint64(ptr)<<32 | uint64(length)
		}).
		Export("host_get_user")

	// host.host_get_room_members(room_ptr i32, room_len i32) -> (resp_ptr i32, resp_len i32)
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, roomPtr, roomLen uint32) uint64 {
			room := readStringFromMemory(mod, roomPtr, roomLen)
			val := wr.hostAPI.GetRoomMembers(ctx, room)
			if val == "" {
				return 0
			}
			ptr, length := writeStringToMemory(mod, val)
			return uint64(ptr)<<32 | uint64(length)
		}).
		Export("host_get_room_members")

	// host.host_add_reaction(room_ptr i32, room_len i32, msg_ptr i32, msg_len i32, emoji_ptr i32, emoji_len i32) -> i32
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, roomPtr, roomLen, msgPtr, msgLen, emojiPtr, emojiLen uint32) int32 {
			room := readStringFromMemory(mod, roomPtr, roomLen)
			msg := readStringFromMemory(mod, msgPtr, msgLen)
			emoji := readStringFromMemory(mod, emojiPtr, emojiLen)
			return wr.hostAPI.AddReaction(ctx, room, msg, emoji)
		}).
		Export("host_add_reaction")

	// host.host_register_slash_command(cmd_ptr i32, cmd_len i32, desc_ptr i32, desc_len i32) -> i32
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, cmdPtr, cmdLen, descPtr, descLen uint32) int32 {
			cmd := readStringFromMemory(mod, cmdPtr, cmdLen)
			desc := readStringFromMemory(mod, descPtr, descLen)
			return wr.hostAPI.RegisterSlashCommand(ctx, cmd, desc)
		}).
		Export("host_register_slash_command")

	// host.host_schedule_timer(delay_ms i32, event_ptr i32, event_len i32, payload_ptr i32, payload_len i32) -> i32
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, delayMs int32, eventPtr, eventLen, payloadPtr, payloadLen uint32) int32 {
			event := readStringFromMemory(mod, eventPtr, eventLen)
			payload := readStringFromMemory(mod, payloadPtr, payloadLen)
			return wr.hostAPI.ScheduleTimer(ctx, delayMs, event, payload)
		}).
		Export("host_schedule_timer")

	// host.host_register_room_type(type_json_ptr i32, type_json_len i32) -> i32
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, typeJSONPtr, typeJSONLen uint32) int32 {
			typeJSON := readStringFromMemory(mod, typeJSONPtr, typeJSONLen)
			return wr.hostAPI.RegisterRoomType(ctx, typeJSON)
		}).
		Export("host_register_room_type")

	// host.host_http_request(method_ptr i32, method_len i32, url_ptr i32, url_len i32, body_ptr i32, body_len i32) -> (resp_ptr i32, resp_len i32)
	hostModule.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, methodPtr, methodLen, urlPtr, urlLen, bodyPtr, bodyLen uint32) uint64 {
			method := readStringFromMemory(mod, methodPtr, methodLen)
			url := readStringFromMemory(mod, urlPtr, urlLen)
			body := readStringFromMemory(mod, bodyPtr, bodyLen)
			appSlug := mod.Name()
			val := wr.hostAPI.HTTPRequest(ctx, appSlug, method, url, body)
			if val == "" {
				return 0
			}
			ptr, length := writeStringToMemory(mod, val)
			return uint64(ptr)<<32 | uint64(length)
		}).
		Export("host_http_request")

	_, err := hostModule.Instantiate(ctx)
	return err
}

// CompilePlugin compiles a WASM file from disk (does NOT instantiate).
func (wr *WasmRuntime) CompilePlugin(ctx context.Context, manifest *PluginManifest) error {
	wasmBytes, err := os.ReadFile(manifest.WasmPath)
	if err != nil {
		return fmt.Errorf("failed to read WASM file '%s': %w", manifest.WasmPath, err)
	}

	compiled, err := wr.runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("failed to compile WASM for '%s': %w", manifest.Slug, err)
	}

	wr.mu.Lock()
	wr.modules[manifest.Slug] = &WasmModule{
		Slug:     manifest.Slug,
		Manifest: manifest,
		Compiled: compiled,
	}
	wr.mu.Unlock()

	log.Printf("[WasmRuntime] Compiled plugin: %s (v%s)", manifest.Name, manifest.Version)
	return nil
}

// InstantiatePlugin creates a running instance of a compiled WASM plugin.
// Called when an admin installs the app.
func (wr *WasmRuntime) InstantiatePlugin(ctx context.Context, slug string) error {
	wr.mu.RLock()
	wm, ok := wr.modules[slug]
	wr.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin '%s' not compiled", slug)
	}

	cfg := wazero.NewModuleConfig().
		WithName(slug).
		WithStartFunctions() // Don't call _start — keep module alive as a reactor for on_hook calls

	mod, err := wr.runtime.InstantiateModule(ctx, wm.Compiled, cfg)
	if err != nil {
		return fmt.Errorf("failed to instantiate WASM for '%s': %w", slug, err)
	}

	wr.mu.Lock()
	wm.Instance = mod
	wr.mu.Unlock()

	log.Printf("[WasmRuntime] Instantiated plugin: %s", slug)
	return nil
}

// CallHook invokes the on_hook exported function of a running WASM plugin.
func (wr *WasmRuntime) CallHook(ctx context.Context, slug, eventName string, payload []byte) (int32, error) {
	wr.mu.RLock()
	wm, ok := wr.modules[slug]
	wr.mu.RUnlock()
	if !ok || wm.Instance == nil {
		return -1, fmt.Errorf("plugin '%s' not instantiated", slug)
	}

	mod := wm.Instance
	onHook := mod.ExportedFunction("on_hook")
	if onHook == nil {
		return -1, fmt.Errorf("plugin '%s' does not export 'on_hook'", slug)
	}

	// Write event name + payload into WASM memory
	eventPtr, eventLen := writeStringToMemory(mod, eventName)
	payloadPtr, payloadLen := writeBytesToMemory(mod, payload)

	results, err := onHook.Call(ctx, uint64(eventPtr), uint64(eventLen), uint64(payloadPtr), uint64(payloadLen))
	if err != nil {
		return -1, fmt.Errorf("on_hook call failed for '%s': %w", slug, err)
	}

	if len(results) > 0 {
		return int32(results[0]), nil
	}
	return 0, nil
}

// UnloadPlugin closes a running WASM instance.
func (wr *WasmRuntime) UnloadPlugin(ctx context.Context, slug string) error {
	wr.mu.Lock()
	defer wr.mu.Unlock()

	wm, ok := wr.modules[slug]
	if !ok {
		return nil
	}

	if wm.Instance != nil {
		if err := wm.Instance.Close(ctx); err != nil {
			log.Printf("[WasmRuntime] Warning: error closing module '%s': %v", slug, err)
		}
		wm.Instance = nil
	}

	log.Printf("[WasmRuntime] Unloaded plugin: %s", slug)
	return nil
}

// IsLoaded checks if a plugin is compiled and instantiated.
func (wr *WasmRuntime) IsLoaded(slug string) bool {
	wr.mu.RLock()
	defer wr.mu.RUnlock()
	wm, ok := wr.modules[slug]
	return ok && wm.Instance != nil
}

// Close shuts down the entire WASM runtime.
func (wr *WasmRuntime) Close(ctx context.Context) error {
	wr.mu.Lock()
	defer wr.mu.Unlock()
	log.Println("[WasmRuntime] Shutting down...")
	return wr.runtime.Close(ctx)
}

// ── Memory Helpers ──────────────────────────────────────────

// readStringFromMemory reads a string from WASM linear memory.
func readStringFromMemory(mod api.Module, ptr, length uint32) string {
	if length == 0 {
		return ""
	}
	mem := mod.Memory()
	if mem == nil {
		return ""
	}
	buf, ok := mem.Read(ptr, length)
	if !ok {
		return ""
	}
	return string(buf)
}

// writeStringToMemory writes a string into WASM memory using the malloc export.
func writeStringToMemory(mod api.Module, s string) (uint32, uint32) {
	return writeBytesToMemory(mod, []byte(s))
}

// writeBytesToMemory writes bytes into WASM memory.
// Requires the plugin to export a "malloc" function.
func writeBytesToMemory(mod api.Module, data []byte) (uint32, uint32) {
	if len(data) == 0 {
		return 0, 0
	}

	malloc := mod.ExportedFunction("malloc")
	if malloc == nil {
		log.Println("[WasmRuntime] Warning: plugin does not export 'malloc'")
		return 0, 0
	}

	results, err := malloc.Call(context.Background(), uint64(len(data)))
	if err != nil || len(results) == 0 {
		log.Printf("[WasmRuntime] Warning: malloc failed: %v", err)
		return 0, 0
	}

	ptr := uint32(results[0])
	mem := mod.Memory()
	if mem == nil {
		return 0, 0
	}

	ok := mem.Write(ptr, data)
	if !ok {
		return 0, 0
	}

	return ptr, uint32(len(data))
}
