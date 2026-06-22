package wasm

import (
	"context"
	"log"
	"net/url"
)

// HostAPI provides the host-side functions that WASM plugins can call.
// Each function is exported into the WASM "host" module namespace.
//
// App identity is passed explicitly via the slug parameter on each call,
// derived from the WASM module instance name (mod.Name()). This avoids
// global mutable state and is safe for concurrent plugin execution.
type HostAPI struct {
	// Callbacks injected by the Engine to connect to actual services.
	// These are nil until the Engine sets them up.
	OnSendMessage func(ctx context.Context, roomID, content, senderName string) error
	OnKVGet       func(ctx context.Context, appID, key string) (string, error)
	OnKVSet       func(ctx context.Context, appID, key, value string) error
	OnHTTPRequest func(ctx context.Context, appID, method, url, body string) (string, error)
	OnGetRoomHistory func(ctx context.Context, roomID string) (string, error)
	OnPublishWSEvent func(ctx context.Context, roomID, eventName, data string) error
	OnGetUserStatus  func(ctx context.Context, userID string) (string, error)

	OnDeleteMessage        func(ctx context.Context, roomID, messageID string) error
	OnEditMessage          func(ctx context.Context, roomID, messageID, newContent string) error
	OnCreateRoom           func(ctx context.Context, name, roomType string) (string, error)
	OnGetUser              func(ctx context.Context, userID string) (string, error)
	OnGetRoomMembers       func(ctx context.Context, roomID string) (string, error)
	OnAddReaction          func(ctx context.Context, roomID, messageID, emoji string) error
	OnRegisterSlashCommand func(ctx context.Context, command, description string) error
	OnScheduleTimer        func(ctx context.Context, delayMs int32, hookEvent string, payload string) error
	OnRegisterRoomType     func(ctx context.Context, typeJSON string) error

	// sandbox enforces outbound HTTP policy (permission + SSRF protection).
	sandbox *Sandbox
}

// NewHostAPI creates a host API with default no-op implementations.
func NewHostAPI() *HostAPI {
	return &HostAPI{sandbox: NewSandbox()}
}

// LogForApp handles the host_log call from WASM plugins.
func (h *HostAPI) LogForApp(level uint32, appSlug string, message string) {
	switch level {
	case 0:
		log.Printf("[Plugin:%s] DEBUG: %s", appSlug, message)
	case 1:
		log.Printf("[Plugin:%s] INFO: %s", appSlug, message)
	case 2:
		log.Printf("[Plugin:%s] WARN: %s", appSlug, message)
	case 3:
		log.Printf("[Plugin:%s] ERROR: %s", appSlug, message)
	default:
		log.Printf("[Plugin:%s] %s", appSlug, message)
	}
}

// SendMessage handles the host_send_message call from WASM plugins.
// Returns 0 on success, negative on error.
func (h *HostAPI) SendMessage(ctx context.Context, roomID, content, senderName string) int32 {
	if h.OnSendMessage == nil {
		log.Printf("[HostAPI] SendMessage not wired: room=%s msg=%s", roomID, content)
		return 0 // return success for now
	}
	if err := h.OnSendMessage(ctx, roomID, content, senderName); err != nil {
		log.Printf("[HostAPI] SendMessage failed: %v", err)
		return -1
	}
	return 0
}

// KVGetForApp handles the host_kv_get call from WASM plugins.
func (h *HostAPI) KVGetForApp(ctx context.Context, appSlug, key string) string {
	if h.OnKVGet == nil {
		log.Printf("[HostAPI] KVGet not wired: app=%s key=%s", appSlug, key)
		return ""
	}
	val, err := h.OnKVGet(ctx, appSlug, key)
	if err != nil {
		log.Printf("[HostAPI] KVGet failed: app=%s key=%s err=%v", appSlug, key, err)
		return ""
	}
	return val
}

// KVSetForApp handles the host_kv_set call from WASM plugins.
func (h *HostAPI) KVSetForApp(ctx context.Context, appSlug, key, value string) int32 {
	if h.OnKVSet == nil {
		log.Printf("[HostAPI] KVSet not wired: app=%s key=%s", appSlug, key)
		return 0
	}
	if err := h.OnKVSet(ctx, appSlug, key, value); err != nil {
		log.Printf("[HostAPI] KVSet failed: app=%s key=%s err=%v", appSlug, key, err)
		return -1
	}
	return 0
}

// HTTPRequest handles the host_http_request call from WASM plugins.
// Allows plugins to make outbound HTTP requests through the host.
func (h *HostAPI) HTTPRequest(ctx context.Context, appSlug, method, rawURL, body string) string {
	if h.OnHTTPRequest == nil {
		log.Printf("[HostAPI] HTTPRequest not wired: app=%s method=%s url=%s", appSlug, method, rawURL)
		return ""
	}

	// Enforce permission + SSRF policy before making any outbound request.
	if err := h.checkHTTPAllowed(appSlug, rawURL); err != nil {
		log.Printf("[HostAPI] HTTPRequest blocked: app=%s url=%s reason=%v", appSlug, rawURL, err)
		return ""
	}

	val, err := h.OnHTTPRequest(ctx, appSlug, method, rawURL, body)
	if err != nil {
		log.Printf("[HostAPI] HTTPRequest failed: app=%s method=%s url=%s err=%v", appSlug, method, rawURL, err)
		return ""
	}
	return val
}

// checkHTTPAllowed validates an outbound request against the plugin's declared
// permissions and the sandbox SSRF block-list (internal/private addresses).
func (h *HostAPI) checkHTTPAllowed(appSlug, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}

	var perms []string
	if manifest := Registry.GetBySlug(appSlug); manifest != nil {
		perms = manifest.Permissions
	}
	adapter := &AppAdapter{Slug: appSlug, Name: appSlug, Permissions: perms}

	sandbox := h.sandbox
	if sandbox == nil {
		sandbox = NewSandbox()
	}
	return sandbox.ValidateHTTPDomain(adapter, parsed.Hostname())
}

// GetRoomHistory handles the host_get_room_history call from WASM plugins.
func (h *HostAPI) GetRoomHistory(ctx context.Context, roomID string) string {
	if h.OnGetRoomHistory == nil {
		log.Printf("[HostAPI] GetRoomHistory not wired")
		return ""
	}
	val, err := h.OnGetRoomHistory(ctx, roomID)
	if err != nil {
		log.Printf("[HostAPI] GetRoomHistory failed: %v", err)
		return ""
	}
	return val
}

// PublishWSEvent handles the host_publish_ws_event call from WASM plugins.
func (h *HostAPI) PublishWSEvent(ctx context.Context, roomID, eventName, data string) int32 {
	if h.OnPublishWSEvent == nil {
		log.Printf("[HostAPI] PublishWSEvent not wired")
		return 0
	}
	if err := h.OnPublishWSEvent(ctx, roomID, eventName, data); err != nil {
		log.Printf("[HostAPI] PublishWSEvent failed: %v", err)
		return -1
	}
	return 0
}

// GetUserStatus handles the host_get_user_status call from WASM plugins.
func (h *HostAPI) GetUserStatus(ctx context.Context, userID string) string {
	if h.OnGetUserStatus == nil {
		log.Printf("[HostAPI] GetUserStatus not wired")
		return ""
	}
	val, err := h.OnGetUserStatus(ctx, userID)
	if err != nil {
		log.Printf("[HostAPI] GetUserStatus failed: %v", err)
		return ""
	}
	return val
}

// DeleteMessage handles the host_delete_message call.
func (h *HostAPI) DeleteMessage(ctx context.Context, roomID, messageID string) int32 {
	if h.OnDeleteMessage == nil {
		log.Printf("[HostAPI] DeleteMessage not wired")
		return -1
	}
	if err := h.OnDeleteMessage(ctx, roomID, messageID); err != nil {
		log.Printf("[HostAPI] DeleteMessage failed: %v", err)
		return -1
	}
	return 0
}

// EditMessage handles the host_edit_message call.
func (h *HostAPI) EditMessage(ctx context.Context, roomID, messageID, newContent string) int32 {
	if h.OnEditMessage == nil {
		log.Printf("[HostAPI] EditMessage not wired")
		return -1
	}
	if err := h.OnEditMessage(ctx, roomID, messageID, newContent); err != nil {
		log.Printf("[HostAPI] EditMessage failed: %v", err)
		return -1
	}
	return 0
}

// CreateRoom handles the host_create_room call.
func (h *HostAPI) CreateRoom(ctx context.Context, name, roomType string) string {
	if h.OnCreateRoom == nil {
		log.Printf("[HostAPI] CreateRoom not wired")
		return ""
	}
	roomID, err := h.OnCreateRoom(ctx, name, roomType)
	if err != nil {
		log.Printf("[HostAPI] CreateRoom failed: %v", err)
		return ""
	}
	return roomID
}

// GetUser handles the host_get_user call.
func (h *HostAPI) GetUser(ctx context.Context, userID string) string {
	if h.OnGetUser == nil {
		log.Printf("[HostAPI] GetUser not wired")
		return ""
	}
	val, err := h.OnGetUser(ctx, userID)
	if err != nil {
		log.Printf("[HostAPI] GetUser failed: %v", err)
		return ""
	}
	return val
}

// GetRoomMembers handles the host_get_room_members call.
func (h *HostAPI) GetRoomMembers(ctx context.Context, roomID string) string {
	if h.OnGetRoomMembers == nil {
		log.Printf("[HostAPI] GetRoomMembers not wired")
		return ""
	}
	val, err := h.OnGetRoomMembers(ctx, roomID)
	if err != nil {
		log.Printf("[HostAPI] GetRoomMembers failed: %v", err)
		return ""
	}
	return val
}

// AddReaction handles the host_add_reaction call.
func (h *HostAPI) AddReaction(ctx context.Context, roomID, messageID, emoji string) int32 {
	if h.OnAddReaction == nil {
		log.Printf("[HostAPI] AddReaction not wired")
		return -1
	}
	if err := h.OnAddReaction(ctx, roomID, messageID, emoji); err != nil {
		log.Printf("[HostAPI] AddReaction failed: %v", err)
		return -1
	}
	return 0
}

// RegisterSlashCommand handles the host_register_slash_command call.
func (h *HostAPI) RegisterSlashCommand(ctx context.Context, command, description string) int32 {
	if h.OnRegisterSlashCommand == nil {
		log.Printf("[HostAPI] RegisterSlashCommand not wired")
		return -1
	}
	if err := h.OnRegisterSlashCommand(ctx, command, description); err != nil {
		log.Printf("[HostAPI] RegisterSlashCommand failed: %v", err)
		return -1
	}
	return 0
}

// ScheduleTimer handles the host_schedule_timer call.
func (h *HostAPI) ScheduleTimer(ctx context.Context, delayMs int32, hookEvent string, payload string) int32 {
	if h.OnScheduleTimer == nil {
		log.Printf("[HostAPI] ScheduleTimer not wired")
		return -1
	}
	if err := h.OnScheduleTimer(ctx, delayMs, hookEvent, payload); err != nil {
		log.Printf("[HostAPI] ScheduleTimer failed: %v", err)
		return -1
	}
	return 0
}

// RegisterRoomType handles the host_register_room_type call.
func (h *HostAPI) RegisterRoomType(ctx context.Context, typeJSON string) int32 {
	if h.OnRegisterRoomType == nil {
		log.Printf("[HostAPI] RegisterRoomType not wired")
		return -1
	}
	if err := h.OnRegisterRoomType(ctx, typeJSON); err != nil {
		log.Printf("[HostAPI] RegisterRoomType failed: %v", err)
		return -1
	}
	return 0
}
