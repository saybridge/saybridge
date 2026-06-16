package actionregistry

import (
	"sync"

	"github.com/saybridge/saybridge/internal/authz"
)

type ActionSlot string

const (
	SlotMessageContextMenu  ActionSlot = "message_context_menu"
	SlotMessageHoverToolbar ActionSlot = "message_hover_toolbar"
	SlotChannelKebabMenu    ActionSlot = "channel_kebab_menu"
	SlotChannelHeaderButton ActionSlot = "channel_header_button"
)

type ActionDefinition struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	Icon        string     `json:"icon"` // lucide-react icon name
	Slot        ActionSlot `json:"slot"`
	Section     string     `json:"section"`    // "default" | "danger"
	Permission  string     `json:"permission"` // Casbin action required or arbitrary permission name
	Source      string     `json:"source"`     // "core" | plugin slug
	SortOrder   int        `json:"sort_order"`
	// Execution
	ActionType  string `json:"action_type"` // "client" | "api" | "ws_hook" | "sdui"
	APIEndpoint string `json:"api_endpoint,omitempty"`
	HookEvent   string `json:"hook_event,omitempty"`
	SDUIScreen  string `json:"sdui_screen,omitempty"`
	// Conditional
	OwnerOnly bool `json:"owner_only,omitempty"` // only show to message owner
	AdminOnly bool `json:"admin_only,omitempty"` // only show to admin
}

// Subject represents the actor checking permissions
type Subject struct {
	ID       string
	Role     string // "admin", "user", "guest"
	RoomRole string // "owner", "moderator", "member"
}

// Object represents the resource being accessed
type Object struct {
	Type       string // "message", "room"
	ID         string
	OwnerID    string // SenderID for message, CreatorID for room
	RoomType   string // "channel", "group", "direct"
	IsReadOnly bool   // whether the room is read-only
}

type ActionRegistry struct {
	actions map[ActionSlot][]ActionDefinition
	mu      sync.RWMutex
}

var DefaultRegistry = NewActionRegistry()

func NewActionRegistry() *ActionRegistry {
	return &ActionRegistry{
		actions: make(map[ActionSlot][]ActionDefinition),
	}
}

// Register adds an action to the registry
func (r *ActionRegistry) Register(action ActionDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slot := action.Slot
	r.actions[slot] = append(r.actions[slot], action)
}

// Unregister removes an action by ID from all slots
func (r *ActionRegistry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for slot, list := range r.actions {
		var newList []ActionDefinition
		for _, act := range list {
			if act.ID != id {
				newList = append(newList, act)
			}
		}
		r.actions[slot] = newList
	}
}

// Clear removes all registered actions
func (r *ActionRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions = make(map[ActionSlot][]ActionDefinition)
}

// GetActions returns all actions registered for a specific slot, filtered by subject & object context.
func (r *ActionRegistry) GetActions(slot ActionSlot, sub Subject, obj Object, enforcer *authz.AuthzEnforcer) []ActionDefinition {
	r.mu.RLock()
	defs := r.actions[slot]
	r.mu.RUnlock()

	var filtered []ActionDefinition

	for _, def := range defs {
		// 1. Check AdminOnly condition
		if def.AdminOnly && sub.Role != "admin" {
			continue
		}

		// 2. Check OwnerOnly condition
		if def.OwnerOnly && obj.OwnerID != "" && sub.ID != obj.OwnerID {
			continue
		}

		// 3. Evaluate permissions with Casbin
		if def.Permission != "" {
			if enforcer == nil {
				// Deny if enforcer is not initialized but permission is required
				continue
			}

			authzSub := authz.Subject{
				ID:       sub.ID,
				Role:     sub.Role,
				RoomRole: sub.RoomRole,
				IsActive: true,
			}

			// Map RoomRole to role for Casbin matching if not admin and RoomRole is set
			if sub.Role != "admin" && sub.RoomRole != "" {
				authzSub.Role = sub.RoomRole
			}

			authzObj := authz.Object{
				Type:       obj.Type,
				ID:         obj.ID,
				OwnerID:    obj.OwnerID,
				RoomType:   obj.RoomType,
				IsReadOnly: obj.IsReadOnly,
			}

			if !enforcer.Can(authzSub, authzObj, def.Permission) {
				continue
			}
		}

		filtered = append(filtered, def)
	}

	return filtered
}
