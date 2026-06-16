package plugin

import (
	"context"

	"github.com/saybridge/saybridge/internal/plugin/actionregistry"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/config"
	"github.com/saybridge/saybridge/pkg/crypto"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Dependencies holds the shared application resources available to plugins.
type Dependencies struct {
	DB               *gorm.DB
	RDB              *redis.Client
	JWTMgr           *crypto.JWTManager
	Cfg              *config.Config
	Hooks            *HookRegistry // Central lifecycle hook registry for event-driven plugin architecture
	SendMessageFn    func(ctx context.Context, roomID, content, senderName string) error
	DeleteMessageFn  func(ctx context.Context, roomID string, timeBucket int, messageID string) error
	GetRoomHistoryFn func(ctx context.Context, roomID string) (string, error)
	PublishWSEventFn func(ctx context.Context, roomID, eventName, data string) error

	// New Host API Callbacks
	EditMessageFn          func(ctx context.Context, userID, roomID, messageID, newContent string) error
	CreateRoomFn           func(ctx context.Context, name, roomType string) (string, error)
	GetUserFn              func(ctx context.Context, userID string) (string, error)
	GetRoomMembersFn       func(ctx context.Context, roomID string) (string, error)
	AddReactionFn          func(ctx context.Context, roomID, messageID, emoji string) error
	RegisterSlashCommandFn func(ctx context.Context, command, description string) error
	ScheduleTimerFn        func(ctx context.Context, delayMs int32, hookEvent string, payload string) error

	Proxy       *PluginProxyRouter
	Actions     *actionregistry.ActionRegistry
	MessageRepo domain.MessageRepository
}

// AdminMenu defines a sidebar entry that a plugin registers in the web admin panel.
type AdminMenu struct {
	Label    string `json:"label"`    // Display text, e.g., "Marketplace"
	Icon     string `json:"icon"`     // Lucide icon name, e.g., "store"
	Priority int    `json:"priority"` // Sort order (lower = higher in sidebar)
}

// UIComponent defines a single UI element to render on mobile via Server-Driven UI (SDUI).
// Each component has a type (e.g., "text_input", "button") and a props map for configuration.
type UIComponent struct {
	Type  string                 `json:"type"` // text_input, button, submit_button, toggle, list, status_card, image, text, search_bar, card_grid, data_table, chip_group, section, tabs, modal, detail_header
	Props map[string]interface{} `json:"props"`
}

// UIScreen defines a full screen composed of UI components.
// Placement determines where the screen appears in the app navigation.
type UIScreen struct {
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	Placement  string        `json:"placement"`                // "settings", "admin", "chat_menu"
	Components []UIComponent `json:"components,omitempty"`
	IframeSrc  string        `json:"iframe_src,omitempty"`     // iframe URL for plugin custom UI
}

// LoginExtension defines UI injected into the login flow by a plugin.
// Used for 2FA verification forms, SSO buttons, etc.
type LoginExtension struct {
	Trigger string        `json:"trigger"`           // e.g., "2fa_required", "sso_button"
	Screen  *UIScreen     `json:"screen,omitempty"`  // Full screen (e.g., 2FA code entry)
	Buttons []UIComponent `json:"buttons,omitempty"` // Inline buttons (e.g., SSO provider buttons on login page)
}

// ComposerExtension defines UI elements a plugin registers inside the message composer.
// The frontend dynamically builds composer plugins from these declarations.
type ComposerExtension struct {
	// Autocomplete triggered by a character (e.g., ":" for emoji, "/" for commands)
	Autocomplete *ComposerAutocomplete `json:"autocomplete,omitempty"`

	// Action button next to the textarea (e.g., emoji picker button)
	ActionButton *ComposerActionButton `json:"action_button,omitempty"`

	// Picker panel that opens when the action button is clicked
	PickerPanel *ComposerPickerPanel `json:"picker_panel,omitempty"`
}

// ComposerAutocomplete defines an autocomplete trigger within the composer.
type ComposerAutocomplete struct {
	Trigger  string `json:"trigger"`   // Trigger character, e.g., ":", "@", "/"
	MinChars int    `json:"min_chars"` // Minimum chars after trigger before searching
	Endpoint string `json:"endpoint"`  // API endpoint for search, e.g., "/api/v1/emoji/custom/search"
}

// ComposerActionButton defines a button rendered next to the composer textarea.
type ComposerActionButton struct {
	Icon    string `json:"icon"`    // Lucide icon name, e.g., "smile"
	Label   string `json:"label"`   // Tooltip text
	Shortcut string `json:"shortcut,omitempty"` // Keyboard shortcut, e.g., "ctrl+e"
}

// ComposerPickerPanel defines a picker popup that opens from the action button.
type ComposerPickerPanel struct {
	Endpoint   string `json:"endpoint"`    // API endpoint to fetch picker data, e.g., "/api/v1/emoji/custom"
	RenderType string `json:"render_type"` // "grid" (emoji grid), "list" (command list), "sdui" (custom SDUI)
}

// UIManifest is the complete UI definition a plugin exposes to clients.
// Mobile apps and web admin panels fetch all manifests via GET /api/v1/plugins/manifest
// and use DynamicRenderer engines to render them as native UI at runtime.
type UIManifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Version     string `json:"version,omitempty"`
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
	Developer   string `json:"developer,omitempty"`
	License     string `json:"license,omitempty"`
	Enabled     bool   `json:"enabled"` // Whether this plugin is active. Clients must respect this flag.

	Screens        []UIScreen      `json:"screens,omitempty"`
	LoginExtension *LoginExtension `json:"login_extension,omitempty"`

	// Admin panel integration — web dynamically builds sidebar + pages from these fields.
	AdminMenu    *AdminMenu `json:"admin_menu,omitempty"`
	AdminScreens []UIScreen `json:"admin_screens,omitempty"`

	// Composer integration — plugins register autocomplete, buttons, and pickers into the message editor.
	ComposerExtensions []ComposerExtension `json:"composer_extensions,omitempty"`

	// UI Extensions and slots mapping
	UIExtensions *UIExtensions `json:"ui_extensions,omitempty"`
}

type UIExtensions struct {
	ChannelActions      []UIAction         `json:"channel_actions,omitempty"`
	MessageHoverActions []UIAction         `json:"message_hover_actions,omitempty"`
	MessageContextMenu  []UIAction         `json:"message_context_menu,omitempty"`
	ChatSidePanels      []PageExtension    `json:"chat_side_panels,omitempty"`
	AdminPages          []PageExtension    `json:"admin_pages,omitempty"`
	SettingsSections    []PageExtension    `json:"settings_sections,omitempty"`
	StandalonePages     []PageExtension    `json:"standalone_pages,omitempty"`
	ComponentSlots      []ComponentSlot    `json:"component_slots,omitempty"`
}

type UIAction struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Icon       string `json:"icon"`                  // lucide-react icon name
	Placement  string `json:"placement,omitempty"`   // "kebab" | "header_button" (channel_actions only)
	Section    string `json:"section,omitempty"`     // "default" | "danger" (context_menu only)
	HookEvent  string `json:"hook_event,omitempty"`  // server hook to fire on click
	SDUIScreen string `json:"sdui_screen,omitempty"` // OR open SDUI screen
}

type PageExtension struct {
	ID           string     `json:"id"`
	Label        string     `json:"label"`
	Icon         string     `json:"icon"`
	SortOrder    int        `json:"sort_order"`
	Render       string     `json:"render"`                 // "data_only" | "sdui" | "iframe" | "component"
	Template     string     `json:"template,omitempty"`      // "item_list" | "user_list" | "data_table" | "settings_form"
	DataEndpoint string     `json:"data_endpoint,omitempty"`
	ItemActions  []UIAction `json:"item_actions,omitempty"`
	SDUIScreen   string     `json:"sdui_screen,omitempty"`
	IframeSrc    string     `json:"iframe_src,omitempty"`   // "/plugins/{slug}/static/index.html"
	Component    string     `json:"component,omitempty"`
}

type ComponentSlot struct {
	Slot          string      `json:"slot"` // e.g. "message_after_body"
	Mode          string      `json:"mode"` // "inject" | "override" | "wrap"
	Render        string      `json:"render"`
	SDUIComponent interface{} `json:"sdui_component,omitempty"`
	IframeSrc     string      `json:"iframe_src,omitempty"`
	SortOrder     int         `json:"sort_order"`
}
