package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	var category string
	var hooks []string

	cmd := &cobra.Command{
		Use:   "init <plugin-name>",
		Short: "Scaffold a new WASM plugin project",
		Long: `Creates a new plugin directory with all required files:
  - main.go (TinyGo WASM template with on_hook export)
  - manifest.json (plugin metadata and configuration)
  - go.mod (Go module file)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			return scaffoldPlugin(name, category, hooks)
		},
	}

	cmd.Flags().StringVarP(&category, "category", "c", "utility", "Plugin category (utility, bot, integration, automation, analytics)")
	cmd.Flags().StringSliceVarP(&hooks, "hooks", "k", []string{"message.after_send"}, "Hook events to subscribe to")

	return cmd
}

func scaffoldPlugin(name, category string, hooks []string) error {
	// Sanitize plugin name
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	dir := slug

	// Check if directory already exists
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("directory %q already exists", dir)
	}

	// Create directory
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Generate main.go
	mainGo := generateMainGo(name, hooks)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	// Generate manifest.json
	manifest := generateManifest(name, slug, category, hooks)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0644); err != nil {
		return fmt.Errorf("write manifest.json: %w", err)
	}

	// Generate go.mod
	goMod := generateGoMod(slug)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	fmt.Printf("✓ Plugin scaffolded: %s/\n", dir)
	fmt.Printf("  ├── main.go          (plugin logic)\n")
	fmt.Printf("  ├── manifest.json    (metadata)\n")
	fmt.Printf("  └── go.mod           (module)\n")
	fmt.Println()
	fmt.Printf("Next steps:\n")
	fmt.Printf("  1. cd %s\n", dir)
	fmt.Printf("  2. Edit main.go to add your plugin logic\n")
	fmt.Printf("  3. Run: chat-cli dev     (hot-reload development)\n")
	fmt.Printf("  4. Run: chat-cli test    (run test events)\n")
	fmt.Printf("  5. Run: chat-cli publish (package for marketplace)\n")

	return nil
}

func generateMainGo(name string, hooks []string) string {
	hookCases := ""
	for _, hook := range hooks {
		hookCases += fmt.Sprintf(`
	if event == "%s" {
		logInfo("%s: received %s event")
		// TODO: Add your plugin logic here
		//
		// Available payload fields depend on the event type:
		//   message.after_send: sender_id, room_id, message_id, content
		//   room.after_create:  room_id, creator_id, name, room_type
		//   user.status_change: user_id, old_status, new_status
		//
		// Available host functions:
		//   sendMessage(roomID, message)       - Send a message to a room
		//   logInfo(message)                   - Log an info message
		//   kvGet(key) / kvSet(key, value)     - Key-value storage
		//   publishWSEvent(room, event, data)  - Publish WebSocket event
		return 0
	}
`, hook, name, hook)
	}

	return fmt.Sprintf(`//go:build tinygo

// %s WASM Plugin
// Build: tinygo build -o plugin.wasm -target wasi ./
package main

import (
	"strings"
	"unsafe"
)

// Host function imports — provided by the Saybridge runtime
//go:wasmimport host host_log
func hostLog(level uint32, msgPtr uint32, msgLen uint32)

//go:wasmimport host host_send_message
func hostSendMessage(roomPtr, roomLen, msgPtr, msgLen uint32) int32

//go:wasmimport host host_kv_get
func hostKVGet(keyPtr, keyLen uint32) uint64

//go:wasmimport host host_kv_set
func hostKVSet(keyPtr, keyLen, valPtr, valLen uint32) int32

//go:wasmimport host host_publish_ws_event
func hostPublishWSEvent(roomPtr, roomLen, eventPtr, eventLen, dataPtr, dataLen uint32) int32

// Memory allocator for host → guest data passing
var allocBuf []byte

//export malloc
func malloc(size uint32) uint32 {
	allocBuf = make([]byte, size)
	return uint32(uintptr(unsafe.Pointer(&allocBuf[0])))
}

//export on_hook
func onHook(eventPtr, eventLen, payloadPtr, payloadLen uint32) int32 {
	event := ptrToString(eventPtr, eventLen)
	_ = ptrToString(payloadPtr, payloadLen) // payload (use getJsonStringField to extract fields)
%s
	return 0
}

// ── Helper Functions ──────────────────────────────────────────────────────────

func sendMessage(room, msg string) {
	roomBytes := []byte(room)
	msgBytes := []byte(msg)
	hostSendMessage(
		uint32(uintptr(unsafe.Pointer(&roomBytes[0]))), uint32(len(roomBytes)),
		uint32(uintptr(unsafe.Pointer(&msgBytes[0]))), uint32(len(msgBytes)),
	)
}

func logInfo(msg string) {
	msgBytes := []byte(msg)
	hostLog(1, uint32(uintptr(unsafe.Pointer(&msgBytes[0]))), uint32(len(msgBytes)))
}

func kvGet(key string) string {
	kb := []byte(key)
	res := hostKVGet(uint32(uintptr(unsafe.Pointer(&kb[0]))), uint32(len(kb)))
	return unpackString(res)
}

func kvSet(key, value string) int32 {
	kb := []byte(key)
	vb := []byte(value)
	return hostKVSet(
		uint32(uintptr(unsafe.Pointer(&kb[0]))), uint32(len(kb)),
		uint32(uintptr(unsafe.Pointer(&vb[0]))), uint32(len(vb)),
	)
}

func publishWSEvent(room, event, data string) int32 {
	roomBytes := []byte(room)
	eventBytes := []byte(event)
	dataBytes := []byte(data)
	return hostPublishWSEvent(
		uint32(uintptr(unsafe.Pointer(&roomBytes[0]))), uint32(len(roomBytes)),
		uint32(uintptr(unsafe.Pointer(&eventBytes[0]))), uint32(len(eventBytes)),
		uint32(uintptr(unsafe.Pointer(&dataBytes[0]))), uint32(len(dataBytes)),
	)
}

func ptrToString(ptr, length uint32) string {
	if length == 0 {
		return ""
	}
	return unsafe.String((*byte)(unsafe.Pointer(uintptr(ptr))), length)
}

func unpackString(val uint64) string {
	if val == 0 {
		return ""
	}
	ptr := uint32(val >> 32)
	length := uint32(val & 0xffffffff)
	return ptrToString(ptr, length)
}

func getJsonStringField(json, field string) string {
	key := "\"" + field + "\":"
	idx := strings.Index(json, key)
	if idx == -1 {
		return ""
	}
	start := idx + len(key)
	for start < len(json) && (json[start] == ' ' || json[start] == ':') {
		start++
	}
	if start >= len(json) {
		return ""
	}
	if json[start] == '"' {
		start++
		end := start
		for end < len(json) && json[end] != '"' {
			end++
		}
		if end < len(json) {
			return json[start:end]
		}
	}
	return ""
}

func main() {}
`, name, hookCases)
}

func generateManifest(name, slug, category string, hooks []string) string {
	hooksJSON := ""
	for i, h := range hooks {
		if i > 0 {
			hooksJSON += ", "
		}
		hooksJSON += fmt.Sprintf(`"%s"`, h)
	}

	return fmt.Sprintf(`{
  "name": "%s",
  "slug": "%s",
  "version": "0.1.0",
  "description": "A Saybridge plugin",
  "author": "",
  "category": "%s",
  "hooks": [%s],
  "permissions": ["send_message", "read_message"],
  "settings": [],
  "ui": {}
}
`, name, slug, category, hooksJSON)
}

func generateGoMod(slug string) string {
	return fmt.Sprintf(`module %s

go 1.25.0
`, slug)
}
