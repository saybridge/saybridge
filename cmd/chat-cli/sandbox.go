package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Sandbox provides a WASM runtime environment for testing plugins.
type Sandbox struct {
	runtime wazero.Runtime
	module  api.Module
	logs    []LogEntry
	msgs    []SentMessage
	kvStore map[string]string
	wsEvts  []WSEvent
}

// LogEntry represents a captured log message from the plugin.
type LogEntry struct {
	Level   uint32
	Message string
	Time    time.Time
}

// SentMessage represents a message sent by the plugin.
type SentMessage struct {
	RoomID  string
	Content string
}

// WSEvent represents a WebSocket event published by the plugin.
type WSEvent struct {
	RoomID string
	Event  string
	Data   string
}

// NewSandbox creates a new WASM sandbox runtime with mock host functions.
func NewSandbox(ctx context.Context) (*Sandbox, error) {
	sb := &Sandbox{
		kvStore: make(map[string]string),
	}

	rt := wazero.NewRuntime(ctx)
	sb.runtime = rt

	// Instantiate WASI
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	// Register mock host functions
	_, err := rt.NewHostModuleBuilder("host").
		NewFunctionBuilder().WithFunc(sb.hostLog).Export("host_log").
		NewFunctionBuilder().WithFunc(sb.hostSendMessage).Export("host_send_message").
		NewFunctionBuilder().WithFunc(sb.hostKVGet).Export("host_kv_get").
		NewFunctionBuilder().WithFunc(sb.hostKVSet).Export("host_kv_set").
		NewFunctionBuilder().WithFunc(sb.hostGetUserStatus).Export("host_get_user_status").
		NewFunctionBuilder().WithFunc(sb.hostPublishWSEvent).Export("host_publish_ws_event").
		NewFunctionBuilder().WithFunc(sb.hostAiQuery).Export("host_ai_query").
		NewFunctionBuilder().WithFunc(sb.hostGetRoomHistory).Export("host_get_room_history").
		Instantiate(ctx)
	if err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("register host functions: %w", err)
	}

	return sb, nil
}

// LoadPlugin loads a compiled WASM plugin into the sandbox.
func (sb *Sandbox) LoadPlugin(ctx context.Context, wasmPath string) error {
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("read wasm file: %w", err)
	}

	mod, err := sb.runtime.Instantiate(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("instantiate wasm: %w", err)
	}
	sb.module = mod
	return nil
}

// CallHook invokes the on_hook export with the given event and payload.
func (sb *Sandbox) CallHook(ctx context.Context, event, payload string) (int32, error) {
	if sb.module == nil {
		return -1, fmt.Errorf("no plugin loaded")
	}

	onHook := sb.module.ExportedFunction("on_hook")
	if onHook == nil {
		return -1, fmt.Errorf("plugin does not export on_hook function")
	}

	mallocFn := sb.module.ExportedFunction("malloc")
	if mallocFn == nil {
		return -1, fmt.Errorf("plugin does not export malloc function")
	}

	// Allocate event string in guest memory
	eventBytes := []byte(event)
	eventRes, err := mallocFn.Call(ctx, uint64(len(eventBytes)))
	if err != nil {
		return -1, fmt.Errorf("malloc for event: %w", err)
	}
	eventPtr := uint32(eventRes[0])
	sb.module.Memory().Write(eventPtr, eventBytes)

	// Allocate payload string in guest memory
	payloadBytes := []byte(payload)
	payloadRes, err := mallocFn.Call(ctx, uint64(len(payloadBytes)))
	if err != nil {
		return -1, fmt.Errorf("malloc for payload: %w", err)
	}
	payloadPtr := uint32(payloadRes[0])
	sb.module.Memory().Write(payloadPtr, payloadBytes)

	// Call on_hook
	results, err := onHook.Call(ctx,
		uint64(eventPtr), uint64(len(eventBytes)),
		uint64(payloadPtr), uint64(len(payloadBytes)),
	)
	if err != nil {
		return -1, fmt.Errorf("call on_hook: %w", err)
	}

	return int32(results[0]), nil
}

// Close releases all sandbox resources.
func (sb *Sandbox) Close(ctx context.Context) {
	if sb.module != nil {
		sb.module.Close(ctx)
	}
	if sb.runtime != nil {
		sb.runtime.Close(ctx)
	}
}

// GetLogs returns all captured log entries.
func (sb *Sandbox) GetLogs() []LogEntry { return sb.logs }

// GetMessages returns all sent messages.
func (sb *Sandbox) GetMessages() []SentMessage { return sb.msgs }

// GetWSEvents returns all published WebSocket events.
func (sb *Sandbox) GetWSEvents() []WSEvent { return sb.wsEvts }

// ── Mock Host Functions ──────────────────────────────────────────────────────

func (sb *Sandbox) hostLog(ctx context.Context, mod api.Module, level, msgPtr, msgLen uint32) {
	msg := readString(mod, msgPtr, msgLen)
	sb.logs = append(sb.logs, LogEntry{Level: level, Message: msg, Time: time.Now()})

	levelStr := "INFO"
	if level == 0 {
		levelStr = "DEBUG"
	} else if level == 2 {
		levelStr = "WARN"
	} else if level == 3 {
		levelStr = "ERROR"
	}
	fmt.Printf("  \033[36m[%s]\033[0m %s\n", levelStr, msg)
}

func (sb *Sandbox) hostSendMessage(ctx context.Context, mod api.Module, roomPtr, roomLen, msgPtr, msgLen uint32) int32 {
	room := readString(mod, roomPtr, roomLen)
	msg := readString(mod, msgPtr, msgLen)
	sb.msgs = append(sb.msgs, SentMessage{RoomID: room, Content: msg})
	fmt.Printf("  \033[32m[MSG → %s]\033[0m %s\n", room, msg)
	return 0
}

func (sb *Sandbox) hostKVGet(ctx context.Context, mod api.Module, keyPtr, keyLen uint32) uint64 {
	key := readString(mod, keyPtr, keyLen)
	val, ok := sb.kvStore[key]
	if !ok {
		return 0
	}
	fmt.Printf("  \033[33m[KV GET]\033[0m %s = %s\n", key, val)
	return writeStringToGuest(mod, val)
}

func (sb *Sandbox) hostKVSet(ctx context.Context, mod api.Module, keyPtr, keyLen, valPtr, valLen uint32) int32 {
	key := readString(mod, keyPtr, keyLen)
	val := readString(mod, valPtr, valLen)
	sb.kvStore[key] = val
	fmt.Printf("  \033[33m[KV SET]\033[0m %s = %s\n", key, val)
	return 0
}

func (sb *Sandbox) hostGetUserStatus(ctx context.Context, mod api.Module, userPtr, userLen uint32) uint64 {
	user := readString(mod, userPtr, userLen)
	fmt.Printf("  \033[35m[USER STATUS]\033[0m %s → online\n", user)
	return writeStringToGuest(mod, "online||Available")
}

func (sb *Sandbox) hostPublishWSEvent(ctx context.Context, mod api.Module, roomPtr, roomLen, eventPtr, eventLen, dataPtr, dataLen uint32) int32 {
	room := readString(mod, roomPtr, roomLen)
	event := readString(mod, eventPtr, eventLen)
	data := readString(mod, dataPtr, dataLen)
	sb.wsEvts = append(sb.wsEvts, WSEvent{RoomID: room, Event: event, Data: data})
	fmt.Printf("  \033[34m[WS EVENT → %s]\033[0m %s: %s\n", room, event, data)
	return 0
}

func (sb *Sandbox) hostAiQuery(ctx context.Context, mod api.Module, promptPtr, promptLen, systemPtr, systemLen uint32) uint64 {
	prompt := readString(mod, promptPtr, promptLen)
	_ = readString(mod, systemPtr, systemLen)
	fmt.Printf("  \033[35m[AI QUERY]\033[0m %s\n", truncate(prompt, 80))
	mockResponse := "This is a mock AI response for testing purposes."
	return writeStringToGuest(mod, mockResponse)
}

func (sb *Sandbox) hostGetRoomHistory(ctx context.Context, mod api.Module, roomPtr, roomLen uint32) uint64 {
	room := readString(mod, roomPtr, roomLen)
	fmt.Printf("  \033[35m[ROOM HISTORY]\033[0m %s\n", room)
	mockHistory := "Alice: Hello everyone\nBob: Hey Alice, how are you?\nAlice: Good! Let's discuss the project."
	return writeStringToGuest(mod, mockHistory)
}

// ── Utility Functions ─────────────────────────────────────────────────────────

func readString(mod api.Module, ptr, length uint32) string {
	if length == 0 {
		return ""
	}
	buf, ok := mod.Memory().Read(ptr, length)
	if !ok {
		return ""
	}
	return string(buf)
}

func writeStringToGuest(mod api.Module, s string) uint64 {
	if s == "" {
		return 0
	}
	mallocFn := mod.ExportedFunction("malloc")
	if mallocFn == nil {
		return 0
	}
	data := []byte(s)
	results, err := mallocFn.Call(context.Background(), uint64(len(data)))
	if err != nil {
		return 0
	}
	ptr := uint32(results[0])
	mod.Memory().Write(ptr, data)
	return (uint64(ptr) << 32) | uint64(len(data))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ── Build Helper ─────────────────────────────────────────────────────────────

// BuildPlugin compiles a TinyGo plugin to WASM.
func BuildPlugin(dir string) (string, error) {
	wasmPath := filepath.Join(dir, "plugin.wasm")

	cmd := exec.Command("tinygo", "build", "-o", wasmPath, "-target", "wasi", "./")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("Building plugin in %s...", dir)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tinygo build failed: %w", err)
	}

	return wasmPath, nil
}
