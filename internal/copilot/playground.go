package copilot

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/plugin"
	"github.com/saybridge/saybridge/pkg/response"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// ── App Playground Backend Handler ────────────────────────────────────────────

func init() {
	plugin.Registry.On(plugin.OnServerStart, plugin.HookHandler{
		Name:     "playground:start",
		Priority: 50,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			api, _ := payload["api"].(*gin.RouterGroup)
			if api == nil {
				return nil, nil
			}

			pgGroup := api.Group("/admin/playground")
			{
				pgGroup.POST("/run", handlePlaygroundRun)
			}
			log.Printf("[Playground] ✓ Routes registered: /api/v1/admin/playground/*")
			return nil, nil
		},
	})
}

type playgroundRunRequest struct {
	SourceCode string `json:"source_code" binding:"required"`
	Event      string `json:"event" binding:"required"`
	Payload    string `json:"payload" binding:"required"`
}

type playgroundLogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type playgroundMessage struct {
	RoomID  string `json:"room_id"`
	Content string `json:"content"`
}

type playgroundWSEvent struct {
	RoomID string `json:"room_id"`
	Event  string `json:"event"`
	Data   string `json:"data"`
}

type playgroundResult struct {
	ReturnCode int32               `json:"return_code"`
	Logs       []playgroundLogEntry  `json:"logs"`
	Messages   []playgroundMessage   `json:"messages"`
	WSEvents   []playgroundWSEvent   `json:"ws_events"`
	BuildError string              `json:"build_error,omitempty"`
	RunError   string              `json:"run_error,omitempty"`
}

func handlePlaygroundRun(c *gin.Context) {
	// Admin-only check — role is set by AuthMiddleware as string
	roleVal, exists := c.Get("role")
	if !exists {
		response.Error(c, http.StatusForbidden, "FORBIDDEN", "Admin permissions required")
		return
	}
	role, _ := roleVal.(string)
	if role != "admin" && role != "super_admin" {
		response.Error(c, http.StatusForbidden, "FORBIDDEN", "Admin permissions required")
		return
	}

	var req playgroundRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	result := runInPlayground(req.SourceCode, req.Event, req.Payload)
	response.JSON(c, http.StatusOK, result)
}

func runInPlayground(sourceCode, event, payload string) *playgroundResult {
	result := &playgroundResult{
		Logs:     make([]playgroundLogEntry, 0),
		Messages: make([]playgroundMessage, 0),
		WSEvents: make([]playgroundWSEvent, 0),
	}

	// Create temp directory for compilation
	tmpDir, err := os.MkdirTemp("", "playground-*")
	if err != nil {
		result.BuildError = fmt.Sprintf("Failed to create temp directory: %v", err)
		return result
	}
	defer os.RemoveAll(tmpDir)

	// Write source code
	mainFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainFile, []byte(sourceCode), 0644); err != nil {
		result.BuildError = fmt.Sprintf("Failed to write source: %v", err)
		return result
	}

	// Write go.mod
	goMod := "module playground\n\ngo 1.25.0\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		result.BuildError = fmt.Sprintf("Failed to write go.mod: %v", err)
		return result
	}

	// Build with tinygo
	wasmPath := filepath.Join(tmpDir, "plugin.wasm")
	cmd := exec.Command("tinygo", "build", "-o", wasmPath, "-target", "wasi", "./")
	cmd.Dir = tmpDir
	buildOutput, err := cmd.CombinedOutput()
	if err != nil {
		result.BuildError = fmt.Sprintf("Build failed: %s\n%s", err.Error(), string(buildOutput))
		return result
	}

	// Load into sandbox
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	// Register mock host functions that capture output
	_, err = rt.NewHostModuleBuilder("host").
		NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, level, msgPtr, msgLen uint32) {
			msg := pgReadString(mod, msgPtr, msgLen)
			levelStr := "info"
			if level == 0 { levelStr = "debug" }
			if level == 2 { levelStr = "warn" }
			if level == 3 { levelStr = "error" }
			result.Logs = append(result.Logs, playgroundLogEntry{Level: levelStr, Message: msg})
		}).Export("host_log").
		NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, roomPtr, roomLen, msgPtr, msgLen uint32) int32 {
			room := pgReadString(mod, roomPtr, roomLen)
			msg := pgReadString(mod, msgPtr, msgLen)
			result.Messages = append(result.Messages, playgroundMessage{RoomID: room, Content: msg})
			return 0
		}).Export("host_send_message").
		NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, keyPtr, keyLen uint32) uint64 {
			return 0
		}).Export("host_kv_get").
		NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, keyPtr, keyLen, valPtr, valLen uint32) int32 {
			return 0
		}).Export("host_kv_set").
		NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, userPtr, userLen uint32) uint64 {
			return pgWriteString(mod, "online||Available")
		}).Export("host_get_user_status").
		NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, roomPtr, roomLen, eventPtr, eventLen, dataPtr, dataLen uint32) int32 {
			room := pgReadString(mod, roomPtr, roomLen)
			evt := pgReadString(mod, eventPtr, eventLen)
			data := pgReadString(mod, dataPtr, dataLen)
			result.WSEvents = append(result.WSEvents, playgroundWSEvent{RoomID: room, Event: evt, Data: data})
			return 0
		}).Export("host_publish_ws_event").
		NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, promptPtr, promptLen, systemPtr, systemLen uint32) uint64 {
			return pgWriteString(mod, "Mock AI response from playground")
		}).Export("host_ai_query").
		NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, roomPtr, roomLen uint32) uint64 {
			return pgWriteString(mod, "Alice: Hello\nBob: Hi there")
		}).Export("host_get_room_history").
		Instantiate(ctx)

	if err != nil {
		result.RunError = fmt.Sprintf("Failed to register host functions: %v", err)
		return result
	}

	// Load WASM
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		result.RunError = fmt.Sprintf("Failed to read WASM: %v", err)
		return result
	}

	mod, err := rt.Instantiate(ctx, wasmBytes)
	if err != nil {
		result.RunError = fmt.Sprintf("Failed to instantiate WASM: %v", err)
		return result
	}
	defer mod.Close(ctx)

	// Call on_hook
	onHook := mod.ExportedFunction("on_hook")
	mallocFn := mod.ExportedFunction("malloc")

	if onHook == nil || mallocFn == nil {
		result.RunError = "Plugin does not export on_hook or malloc"
		return result
	}

	// Allocate event
	eventBytes := []byte(event)
	eventRes, _ := mallocFn.Call(ctx, uint64(len(eventBytes)))
	eventPtr := uint32(eventRes[0])
	mod.Memory().Write(eventPtr, eventBytes)

	// Allocate payload
	payloadBytes := []byte(payload)
	payloadRes, _ := mallocFn.Call(ctx, uint64(len(payloadBytes)))
	payloadPtr := uint32(payloadRes[0])
	mod.Memory().Write(payloadPtr, payloadBytes)

	// Execute
	results, err := onHook.Call(ctx,
		uint64(eventPtr), uint64(len(eventBytes)),
		uint64(payloadPtr), uint64(len(payloadBytes)),
	)
	if err != nil {
		result.RunError = fmt.Sprintf("Execution error: %v", err)
		return result
	}

	result.ReturnCode = int32(results[0])
	return result
}

func pgReadString(mod api.Module, ptr, length uint32) string {
	if length == 0 {
		return ""
	}
	buf, ok := mod.Memory().Read(ptr, length)
	if !ok {
		return ""
	}
	return string(buf)
}

func pgWriteString(mod api.Module, s string) uint64 {
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
