package plugin

import "context"

// PluginAPI defines the stable API surface available to all plugins.
// This replaces the scattered function fields in Dependencies with a clean interface
// that can be versioned, mocked, and tested independently.
//
// Inspired by Goravel's app.Make() pattern — plugins receive PluginAPI and call
// well-defined methods instead of accessing raw infrastructure directly.
type PluginAPI interface {
	// ── Messages ─────────────────────────────────────────────────────────
	// SendMessage sends a message to a room on behalf of the plugin (bot identity).
	SendMessage(ctx context.Context, roomID, content, senderName string) error

	// EditMessage edits an existing message.
	EditMessage(ctx context.Context, userID, roomID, messageID, newContent string) error

	// DeleteMessage soft-deletes a message.
	DeleteMessage(ctx context.Context, roomID string, timeBucket int, messageID string) error

	// GetRoomHistory returns the last N messages as a formatted string.
	GetRoomHistory(ctx context.Context, roomID string) (string, error)

	// ── Rooms ────────────────────────────────────────────────────────────
	// CreateRoom creates a new room and returns the room ID.
	CreateRoom(ctx context.Context, name, roomType string) (string, error)

	// GetRoomMembers returns room member list as JSON string.
	GetRoomMembers(ctx context.Context, roomID string) (string, error)

	// ── Users ────────────────────────────────────────────────────────────
	// GetUser returns user info as JSON string.
	GetUser(ctx context.Context, userID string) (string, error)

	// ── Reactions ────────────────────────────────────────────────────────
	// AddReaction adds an emoji reaction to a message.
	AddReaction(ctx context.Context, roomID, messageID, emoji string) error

	// ── Real-time ────────────────────────────────────────────────────────
	// PublishWSEvent publishes a real-time event to a room via NATS/WebSocket.
	PublishWSEvent(ctx context.Context, roomID, eventName, data string) error

	// ── Slash Commands ───────────────────────────────────────────────────
	// RegisterSlashCommand registers a custom slash command.
	RegisterSlashCommand(ctx context.Context, command, description string) error

	// ── Timers ───────────────────────────────────────────────────────────
	// ScheduleTimer schedules a delayed hook event with a payload.
	ScheduleTimer(ctx context.Context, delayMs int32, hookEvent string, payload string) error

	// ── KV Storage ───────────────────────────────────────────────────────
	// KVGet retrieves a value from plugin-scoped key-value storage.
	KVGet(ctx context.Context, key string) (string, error)

	// KVSet stores a value in plugin-scoped key-value storage.
	KVSet(ctx context.Context, key, value string) error

	// KVDelete deletes a key from plugin-scoped key-value storage.
	KVDelete(ctx context.Context, key string) error
}

// pluginAPIAdapter implements PluginAPI by delegating to the existing Dependencies struct.
// This allows incremental migration: existing plugin code keeps working while new code
// uses the clean PluginAPI interface.
type pluginAPIAdapter struct {
	deps *Dependencies
	slug string // Plugin slug for scoped KV storage
}

// NewPluginAPI wraps Dependencies into a PluginAPI interface for a specific plugin.
func NewPluginAPI(deps *Dependencies, slug string) PluginAPI {
	return &pluginAPIAdapter{deps: deps, slug: slug}
}

func (a *pluginAPIAdapter) SendMessage(ctx context.Context, roomID, content, senderName string) error {
	if a.deps.SendMessageFn == nil {
		return nil
	}
	return a.deps.SendMessageFn(ctx, roomID, content, senderName)
}

func (a *pluginAPIAdapter) EditMessage(ctx context.Context, userID, roomID, messageID, newContent string) error {
	if a.deps.EditMessageFn == nil {
		return nil
	}
	return a.deps.EditMessageFn(ctx, userID, roomID, messageID, newContent)
}

func (a *pluginAPIAdapter) DeleteMessage(ctx context.Context, roomID string, timeBucket int, messageID string) error {
	if a.deps.DeleteMessageFn == nil {
		return nil
	}
	return a.deps.DeleteMessageFn(ctx, roomID, timeBucket, messageID)
}

func (a *pluginAPIAdapter) GetRoomHistory(ctx context.Context, roomID string) (string, error) {
	if a.deps.GetRoomHistoryFn == nil {
		return "", nil
	}
	return a.deps.GetRoomHistoryFn(ctx, roomID)
}

func (a *pluginAPIAdapter) CreateRoom(ctx context.Context, name, roomType string) (string, error) {
	if a.deps.CreateRoomFn == nil {
		return "", nil
	}
	return a.deps.CreateRoomFn(ctx, name, roomType)
}

func (a *pluginAPIAdapter) GetRoomMembers(ctx context.Context, roomID string) (string, error) {
	if a.deps.GetRoomMembersFn == nil {
		return "", nil
	}
	return a.deps.GetRoomMembersFn(ctx, roomID)
}

func (a *pluginAPIAdapter) GetUser(ctx context.Context, userID string) (string, error) {
	if a.deps.GetUserFn == nil {
		return "", nil
	}
	return a.deps.GetUserFn(ctx, userID)
}

func (a *pluginAPIAdapter) AddReaction(ctx context.Context, roomID, messageID, emoji string) error {
	if a.deps.AddReactionFn == nil {
		return nil
	}
	return a.deps.AddReactionFn(ctx, roomID, messageID, emoji)
}

func (a *pluginAPIAdapter) PublishWSEvent(ctx context.Context, roomID, eventName, data string) error {
	if a.deps.PublishWSEventFn == nil {
		return nil
	}
	return a.deps.PublishWSEventFn(ctx, roomID, eventName, data)
}

func (a *pluginAPIAdapter) RegisterSlashCommand(ctx context.Context, command, description string) error {
	if a.deps.RegisterSlashCommandFn == nil {
		return nil
	}
	return a.deps.RegisterSlashCommandFn(ctx, command, description)
}

func (a *pluginAPIAdapter) ScheduleTimer(ctx context.Context, delayMs int32, hookEvent string, payload string) error {
	if a.deps.ScheduleTimerFn == nil {
		return nil
	}
	return a.deps.ScheduleTimerFn(ctx, delayMs, hookEvent, payload)
}

func (a *pluginAPIAdapter) KVGet(ctx context.Context, key string) (string, error) {
	scopedKey := "plugin:" + a.slug + ":" + key
	return a.deps.RDB.Get(ctx, scopedKey).Result()
}

func (a *pluginAPIAdapter) KVSet(ctx context.Context, key, value string) error {
	scopedKey := "plugin:" + a.slug + ":" + key
	return a.deps.RDB.Set(ctx, scopedKey, value, 0).Err()
}

func (a *pluginAPIAdapter) KVDelete(ctx context.Context, key string) error {
	scopedKey := "plugin:" + a.slug + ":" + key
	return a.deps.RDB.Del(ctx, scopedKey).Err()
}
