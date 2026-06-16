package domain

import (
	"sync"
)

// RoomTypeDefinition defines the configuration structure of a room type.
type RoomTypeDefinition struct {
	Type        string   `json:"type"`        // "channel", "group", "direct", "team", "discussion"
	Label       string   `json:"label"`
	Icon        string   `json:"icon"`
	Permissions []string `json:"permissions"`
	MaxMembers  int      `json:"max_members"` // 0 = unlimited
	IsPlugin    bool     `json:"is_plugin"`   // registered by plugin or core
}

var (
	roomTypeRegistryMu sync.RWMutex
	RoomTypeRegistry   = map[string]RoomTypeDefinition{
		"channel": {Type: "channel", Label: "Public Channel", Icon: "Hash", MaxMembers: 0, IsPlugin: false},
		"group":   {Type: "group", Label: "Private Group", Icon: "Lock", MaxMembers: 0, IsPlugin: false},
		"direct":  {Type: "direct", Label: "Direct Message", Icon: "MessageSquare", MaxMembers: 2, IsPlugin: false},
	}
)

// RegisterRoomType registers a new room type configuration dynamically.
func RegisterRoomType(def RoomTypeDefinition) {
	roomTypeRegistryMu.Lock()
	defer roomTypeRegistryMu.Unlock()
	RoomTypeRegistry[def.Type] = def
}

// ValidateRoomType checks if a room type exists in the registry.
func ValidateRoomType(t string) bool {
	roomTypeRegistryMu.RLock()
	defer roomTypeRegistryMu.RUnlock()
	_, exists := RoomTypeRegistry[t]
	return exists
}

// GetRoomType retrieves a room type definition from the registry.
func GetRoomType(t string) (RoomTypeDefinition, bool) {
	roomTypeRegistryMu.RLock()
	defer roomTypeRegistryMu.RUnlock()
	def, exists := RoomTypeRegistry[t]
	return def, exists
}

// ListRoomTypes returns all registered room type definitions.
func ListRoomTypes() []RoomTypeDefinition {
	roomTypeRegistryMu.RLock()
	defer roomTypeRegistryMu.RUnlock()
	list := make([]RoomTypeDefinition, 0, len(RoomTypeRegistry))
	for _, def := range RoomTypeRegistry {
		list = append(list, def)
	}
	return list
}
