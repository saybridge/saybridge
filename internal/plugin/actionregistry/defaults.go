package actionregistry

func init() {
	RegisterDefaults()
}

// RegisterDefaults registers the core system actions into the DefaultRegistry.
func RegisterDefaults() {
	DefaultRegistry.Clear()

	// ═══ Message Context Menu (core) ═══
	DefaultRegistry.Register(ActionDefinition{
		ID: "reply", Label: "Reply", Icon: "Reply",
		Slot: SlotMessageContextMenu, Source: "core", SortOrder: 10,
		ActionType: "client", Permission: "reply_message", Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "thread", Label: "Open thread", Icon: "MessageCircle",
		Slot: SlotMessageContextMenu, Source: "core", SortOrder: 20,
		ActionType: "client", Permission: "reply_message", Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "edit", Label: "Edit", Icon: "Edit2",
		Slot: SlotMessageContextMenu, Source: "core", SortOrder: 30,
		ActionType: "client", Permission: "edit_own_message", OwnerOnly: true, Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "copy_text", Label: "Copy message", Icon: "Copy",
		Slot: SlotMessageContextMenu, Source: "core", SortOrder: 60,
		ActionType: "client", Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "copy_link", Label: "Copy link", Icon: "Link",
		Slot: SlotMessageContextMenu, Source: "core", SortOrder: 70,
		ActionType: "client", Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "delete", Label: "Delete message", Icon: "Trash2",
		Slot: SlotMessageContextMenu, Source: "core", SortOrder: 100,
		ActionType: "client", Permission: "delete_own_message", OwnerOnly: true, Section: "danger",
	})

	// ═══ Message Hover Toolbar (core) ═══
	DefaultRegistry.Register(ActionDefinition{
		ID: "react", Label: "React", Icon: "Smile",
		Slot: SlotMessageHoverToolbar, Source: "core", SortOrder: 10,
		ActionType: "client", Permission: "react_message", Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "reply", Label: "Reply", Icon: "CornerUpLeft",
		Slot: SlotMessageHoverToolbar, Source: "core", SortOrder: 20,
		ActionType: "client", Permission: "reply_message", Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "thread", Label: "Thread", Icon: "MessageSquare",
		Slot: SlotMessageHoverToolbar, Source: "core", SortOrder: 30,
		ActionType: "client", Permission: "reply_message", Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "more", Label: "More", Icon: "MoreHorizontal",
		Slot: SlotMessageHoverToolbar, Source: "core", SortOrder: 100,
		ActionType: "client", Section: "default",
	})

	// ═══ Channel Header Buttons (core) ═══
	DefaultRegistry.Register(ActionDefinition{
		ID: "info", Label: "Info", Icon: "Info",
		Slot: SlotChannelHeaderButton, Source: "core", SortOrder: 10,
		ActionType: "client", Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "search", Label: "Search", Icon: "Search",
		Slot: SlotChannelHeaderButton, Source: "core", SortOrder: 20,
		ActionType: "client", Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "members", Label: "Members", Icon: "Users",
		Slot: SlotChannelHeaderButton, Source: "core", SortOrder: 30,
		ActionType: "client", Section: "default",
	})

	// ═══ Channel Kebab Menu (core) ═══
	DefaultRegistry.Register(ActionDefinition{
		ID: "mute", Label: "Mute/Unmute", Icon: "BellOff",
		Slot: SlotChannelKebabMenu, Source: "core", SortOrder: 5,
		ActionType: "api", APIEndpoint: "POST /rooms/{room_id}/mute", Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "notif_prefs", Label: "Notification Preferences", Icon: "Bell",
		Slot: SlotChannelKebabMenu, Source: "core", SortOrder: 10,
		ActionType: "client", Section: "default",
	})
	DefaultRegistry.Register(ActionDefinition{
		ID: "leave", Label: "Leave room", Icon: "LogOut",
		Slot: SlotChannelKebabMenu, Source: "core", SortOrder: 100,
		ActionType: "api", APIEndpoint: "POST /rooms/{room_id}/leave", Section: "danger",
	})
}
