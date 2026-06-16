package authz

import (
	"fmt"
	"sync"

	"github.com/casbin/casbin/v2"
)

// Subject represents the entity requesting access.
type Subject struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	RoomRole   string `json:"room_role"`
	IsActive   bool   `json:"is_active"`
	IsPlugin   bool   `json:"is_plugin"`
	IsOfficial bool   `json:"is_official"`
}

// Object represents the resource being accessed.
type Object struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	OwnerID     string `json:"owner_id"`
	RoomType    string `json:"room_type"`
	MemberCount int    `json:"member_count"`
	IsReadOnly  bool   `json:"is_readonly"`
}

// Policy represents a rule in Casbin.
type Policy struct {
	Role    string `json:"role"`
	ObjType string `json:"obj_type"`
	Action  string `json:"action"`
	Effect  string `json:"effect"`
}

// AuthzEnforcer wraps the Casbin enforcer with thread-safe operations.
type AuthzEnforcer struct {
	enforcer *casbin.Enforcer
	mu       sync.RWMutex
}

// NewEnforcer creates a new AuthzEnforcer using the provided model and policy file paths.
func NewEnforcer(modelPath, policyPath string) (*AuthzEnforcer, error) {
	enf, err := casbin.NewEnforcer(modelPath, policyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create casbin enforcer: %w", err)
	}
	return &AuthzEnforcer{
		enforcer: enf,
	}, nil
}

// toMap converts Subject to a map with lowercase keys matching matcher attributes.
func (s Subject) toMap() map[string]interface{} {
	return map[string]interface{}{
		"id":          s.ID,
		"role":        s.Role,
		"room_role":   s.RoomRole,
		"is_active":   s.IsActive,
		"is_plugin":   s.IsPlugin,
		"is_official": s.IsOfficial,
	}
}

// toMap converts Object to a map with lowercase keys matching matcher attributes.
func (o Object) toMap() map[string]interface{} {
	return map[string]interface{}{
		"type":         o.Type,
		"id":           o.ID,
		"owner_id":     o.OwnerID,
		"room_type":    o.RoomType,
		"member_count": o.MemberCount,
		"is_readonly":  o.IsReadOnly,
	}
}

// Can checks if the Subject has permission to perform Action on the Object.
func (e *AuthzEnforcer) Can(subject Subject, object Object, action string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	allowed, err := e.enforcer.Enforce(subject.toMap(), object.toMap(), action)
	if err != nil {
		// Log error or print to stderr, but deny by default
		fmt.Printf("[AuthzEnforcer] Enforce error: %v\n", err)
		return false
	}
	return allowed
}

// ReloadPolicy reloads the policy from file.
func (e *AuthzEnforcer) ReloadPolicy() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.enforcer.LoadPolicy()
}

// AddPolicy adds a policy rule and persists it.
func (e *AuthzEnforcer) AddPolicy(role, objType, action, effect string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err := e.enforcer.AddPolicy(role, objType, action, effect)
	if err != nil {
		return err
	}
	return e.enforcer.SavePolicy()
}

// RemovePolicy removes a policy rule and persists it.
func (e *AuthzEnforcer) RemovePolicy(role, objType, action, effect string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, err := e.enforcer.RemovePolicy(role, objType, action, effect)
	if err != nil {
		return err
	}
	return e.enforcer.SavePolicy()
}

// GetPoliciesForRole retrieves all policies associated with a role.
func (e *AuthzEnforcer) GetPoliciesForRole(role string) []Policy {
	e.mu.RLock()
	defer e.mu.RUnlock()

	lines, _ := e.enforcer.GetFilteredPolicy(0, role)
	policies := make([]Policy, 0, len(lines))
	for _, line := range lines {
		if len(line) >= 4 {
			policies = append(policies, Policy{
				Role:    line[0],
				ObjType: line[1],
				Action:  line[2],
				Effect:  line[3],
			})
		}
	}
	return policies
}

// GetAllPolicies retrieves all policy rules.
func (e *AuthzEnforcer) GetAllPolicies() []Policy {
	e.mu.RLock()
	defer e.mu.RUnlock()

	lines, err := e.enforcer.GetPolicy()
	if err != nil {
		return nil
	}
	policies := make([]Policy, 0, len(lines))
	for _, line := range lines {
		if len(line) >= 4 {
			policies = append(policies, Policy{
				Role:    line[0],
				ObjType: line[1],
				Action:  line[2],
				Effect:  line[3],
			})
		}
	}
	return policies
}
