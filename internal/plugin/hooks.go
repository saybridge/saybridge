package plugin

import (
	"context"
	"sort"
	"sync"

	"github.com/rs/zerolog/log"
)

// HookEvent defines all lifecycle hook points in the system.
// Core emits these events; plugins register handlers for them.
type HookEvent string

const (
	// === Auth Lifecycle ===

	// PreLogin fires after credentials are validated but before tokens are issued.
	// Payload keys: "user_id" (string)
	// If any handler returns error, login is halted.
	PreLogin HookEvent = "auth.pre_login"

	// PostLogin fires after a user successfully logs in and tokens are issued.
	// Payload keys: "user_id", "device_id", "device_name", "ip_address", "user_agent" (all string)
	// Errors are logged but do not block the login response.
	PostLogin HookEvent = "auth.post_login"

	// AuthenticateExternal fires when local credentials fail, allowing external providers (LDAP, etc.).
	// Payload keys: "email", "password" (string)
	// Handlers should return (*domain.User, nil) on success or (nil, error) on failure.
	AuthenticateExternal HookEvent = "auth.authenticate_external"

	// PreRegister fires before a new user is persisted.
	// Payload keys: "username", "email", "display_name" (string)
	PreRegister HookEvent = "auth.pre_register"

	// PostRegister fires after a new user is successfully created.
	// Payload keys: "user_id", "username", "email" (string)
	PostRegister HookEvent = "auth.post_register"

	// OnLogout fires when a user logs out and their session is destroyed.
	// Payload keys: "user_id", "refresh_token" (string)
	OnLogout HookEvent = "auth.on_logout"

	// === Message Lifecycle ===

	// BeforeSendMessage fires before a message is persisted to the data store.
	// Payload keys: "sender_id", "room_id", "content", "msg_type" (string)
	BeforeSendMessage HookEvent = "message.before_send"

	// AfterSendMessage fires after a message is successfully persisted and broadcast.
	// Payload keys: "sender_id", "room_id", "message_id", "content" (string)
	AfterSendMessage HookEvent = "message.after_send"

	// MessageSlashCommand fires when a message starting with '/' is sent (e.g. slash command).
	// Payload keys: "command", "args", "sender_id", "room_id", "message_id", "content" (string)
	MessageSlashCommand HookEvent = "message.slash_command"

	// BeforeEditMessage fires before a message edit is committed.
	// Payload keys: "user_id", "room_id", "message_id", "new_content" (string)
	BeforeEditMessage HookEvent = "message.before_edit"

	// BeforeDeleteMessage fires before a message is soft-deleted.
	// Payload keys: "user_id", "room_id", "message_id" (string)
	BeforeDeleteMessage HookEvent = "message.before_delete"

	// AfterEditMessage fires after a message edit is committed.
	AfterEditMessage HookEvent = "message.after_edit"

	// AfterDeleteMessage fires after a message is soft-deleted.
	AfterDeleteMessage HookEvent = "message.after_delete"

	// OnReactionToggled fires when a reaction is added or removed.
	OnReactionToggled HookEvent = "message.reaction_toggled"

	// === Room Lifecycle ===

	// BeforeCreateRoom fires before a new room is created.
	// Payload keys: "creator_id", "name", "room_type" (string)
	BeforeCreateRoom HookEvent = "room.before_create"

	// AfterCreateRoom fires after a room is successfully created.
	// Payload keys: "room_id", "creator_id", "name", "room_type" (string)
	AfterCreateRoom HookEvent = "room.after_create"

	// OnMemberJoin fires when a user joins a room.
	// Payload keys: "room_id", "user_id", "operator_id" (string)
	OnMemberJoin HookEvent = "room.member_join"

	// OnMemberLeave fires when a user leaves or is removed from a room.
	// Payload keys: "room_id", "user_id", "operator_id" (string)
	OnMemberLeave HookEvent = "room.member_leave"

	// OnRoomSettingsChanged fires when room settings like E2EE or read-only are changed.
	OnRoomSettingsChanged HookEvent = "room.settings_changed"

	// === File Lifecycle ===

	// OnFileUploaded fires when a file upload is completed.
	OnFileUploaded HookEvent = "file.uploaded"

	// === Search Lifecycle ===

	// AfterSearchQuery fires after a message search is executed.
	AfterSearchQuery HookEvent = "search.after_query"

	// === Notification Lifecycle ===

	// BeforeNotify fires before a notification is dispatched.
	BeforeNotify HookEvent = "notification.before_send"

	// AfterNotify fires after a notification has been sent.
	AfterNotify HookEvent = "notification.after_send"

	// === User Lifecycle ===

	// OnUserStatusChange fires when a user's presence status changes.
	// Payload keys: "user_id", "old_status", "new_status" (string)
	OnUserStatusChange HookEvent = "user.status_change"

	// OnProfileUpdate fires after a user profile is updated.
	// Payload keys: "user_id", "display_name", "avatar_url" (string)
	OnProfileUpdate HookEvent = "user.profile_update"

	// === Plugin Lifecycle ===

	// BeforeUninstallPlugin fires before a plugin is uninstalled.
	// Payload keys: "slug" (string), "name" (string)
	// If any handler returns error, the uninstall is BLOCKED.
	// Use case: marketplace blocks its own removal if marketplace-managed plugins are active.
	BeforeUninstallPlugin HookEvent = "plugin.before_uninstall"

	// AfterUninstallPlugin fires after a plugin is successfully uninstalled.
	// Payload keys: "slug" (string), "name" (string)
	// Errors are logged but do not block.
	AfterUninstallPlugin HookEvent = "plugin.after_uninstall"

	// OnPluginAction fires when a user interacts with a registered plugin UI extension.
	OnPluginAction HookEvent = "plugin.action"

	// === System Lifecycle ===

	// OnServerStart fires when the application server has finished bootstrapping.
	OnServerStart HookEvent = "system.server_start"

	// OnServerShutdown fires during graceful shutdown before connections are closed.
	OnServerShutdown HookEvent = "system.server_shutdown"

	// OnCronTick fires periodically on a system cron interval.
	OnCronTick HookEvent = "system.cron_tick"
)

type HookRuntime string

const (
	RuntimeNative HookRuntime = "native" // Go in-process, 0ms overhead
	RuntimeWASM   HookRuntime = "wasm"   // WASM sandbox, ~0.1-1ms overhead
)

// HookHandler represents a single plugin's handler for a lifecycle event.
type HookHandler struct {
	Name     string      // Plugin name for logging/debugging
	Priority int         // Execution order: lower value = higher priority (0-100)
	Runtime  HookRuntime // Runtime environment for this hook: native or wasm
	Fn       func(ctx context.Context, payload map[string]interface{}) (interface{}, error)
}

// HookRegistry is the central event bus that holds all registered lifecycle handlers.
// It is thread-safe and supports concurrent reads with exclusive writes.
// Supports both untyped (map[string]interface{}) and typed (struct) handlers.
type HookRegistry struct {
	mu             sync.RWMutex
	handlers       map[HookEvent][]HookHandler
	typedHandlers  map[HookEvent][]TypedHookHandler
}

// Registry is the global singleton hook registry instance.
// Plugins register handlers at init/Init time; core emits events at runtime.
var Registry = NewHookRegistry()

// NewHookRegistry creates a new empty HookRegistry.
func NewHookRegistry() *HookRegistry {
	return &HookRegistry{
		handlers:      make(map[HookEvent][]HookHandler),
		typedHandlers: make(map[HookEvent][]TypedHookHandler),
	}
}

// On registers a handler for a specific lifecycle event.
// Handlers are sorted by priority after each registration (lower = first).
func (r *HookRegistry) On(event HookEvent, handler HookHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[event] = append(r.handlers[event], handler)

	// Keep handlers sorted by priority (ascending)
	sort.Slice(r.handlers[event], func(i, j int) bool {
		return r.handlers[event][i].Priority < r.handlers[event][j].Priority
	})

	log.Info().Msgf("[Hook Registry] Registered handler '%s' for event '%s' (priority: %d)", handler.Name, event, handler.Priority)
}

// Emit fires all handlers for an event synchronously in priority order.
// Returns on first error (fail-fast semantics for Pre* hooks).
// If no handlers are registered, returns nil (no-op).
func (r *HookRegistry) Emit(ctx context.Context, event HookEvent, payload map[string]interface{}) error {
	r.mu.RLock()
	handlers := r.handlers[event]
	r.mu.RUnlock()

	if len(handlers) == 0 {
		return nil
	}

	for _, h := range handlers {
		_, err := h.Fn(ctx, payload)
		if err != nil {
			log.Error().Err(err).Msgf("[Hook Registry] Handler '%s' for event '%s' returned error", h.Name, event)
			return err
		}
	}

	return nil
}

// EmitCollect fires all handlers for an event and collects non-nil results.
// Used for events like AuthenticateExternal where the first successful result wins.
// Returns results slice and nil error on first success, or aggregated error if all fail.
func (r *HookRegistry) EmitCollect(ctx context.Context, event HookEvent, payload map[string]interface{}) (interface{}, error) {
	r.mu.RLock()
	handlers := r.handlers[event]
	r.mu.RUnlock()

	if len(handlers) == 0 {
		return nil, nil
	}

	var lastErr error
	for _, h := range handlers {
		result, err := h.Fn(ctx, payload)
		if err == nil && result != nil {
			// First successful result wins
			log.Info().Msgf("[Hook Registry] Handler '%s' for event '%s' returned successful result", h.Name, event)
			return result, nil
		}
		if err != nil {
			lastErr = err
		}
	}

	return nil, lastErr
}

// EmitAsync fires all handlers for an event in separate goroutines.
// Used for Post* hooks where errors are logged but do not block the caller.
func (r *HookRegistry) EmitAsync(ctx context.Context, event HookEvent, payload map[string]interface{}) {
	r.mu.RLock()
	handlers := r.handlers[event]
	r.mu.RUnlock()

	if len(handlers) == 0 {
		return
	}

	for _, h := range handlers {
		go func(handler HookHandler) {
			_, err := handler.Fn(ctx, payload)
			if err != nil {
				log.Error().Err(err).Msgf("[Hook Registry] Async handler '%s' for event '%s' returned error", handler.Name, event)
			}
		}(h)
	}
}

// HasHandlers checks if any handlers are registered for the given event.
func (r *HookRegistry) HasHandlers(event HookEvent) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers[event]) > 0
}

// HandlerCount returns the number of registered handlers for an event.
func (r *HookRegistry) HandlerCount(event HookEvent) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers[event])
}
