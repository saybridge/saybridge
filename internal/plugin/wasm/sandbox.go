package wasm

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// Sandbox enforces security constraints on marketplace app execution.
type Sandbox struct {
	ExecutionTimeout time.Duration // Max execution time per hook handler (default: 5s)
	MaxHTTPDomains   int           // Max allowed outgoing HTTP domains per app
}

// NewSandbox creates a sandbox with default security settings.
func NewSandbox() *Sandbox {
	return &Sandbox{
		ExecutionTimeout: 5 * time.Second,
		MaxHTTPDomains:   10,
	}
}

// PermissionMap defines what each permission grants access to.
var PermissionMap = map[string]string{
	"messages.read":    "Read messages in rooms the app is installed in",
	"messages.write":   "Send messages on behalf of the app",
	"rooms.list":       "List rooms the user has access to",
	"rooms.create":     "Create new rooms",
	"users.read":       "Read user profile information",
	"users.presence":   "Access user presence/online status",
	"http.outgoing":    "Make HTTP requests to external services",
	"storage.read":     "Read from app KV storage",
	"storage.write":    "Write to app KV storage",
	"files.upload":     "Upload files to S3 storage",
	"admin.settings":   "Access admin-level settings",
}

// EventPermissionRequirements maps hook events to required permissions.
var EventPermissionRequirements = map[string][]string{
	"message.before_send":      {"messages.read"},
	"message.after_send":       {"messages.read"},
	"message.slash_command":     {"messages.read"},
	"message.before_edit":      {"messages.read"},
	"message.after_edit":       {"messages.read"},
	"message.before_delete":    {"messages.read"},
	"message.after_delete":     {"messages.read"},
	"message.reaction_toggled": {"messages.read"},
	"room.before_create":       {"rooms.list"},
	"room.after_create":        {"rooms.list"},
	"room.member_join":         {"rooms.list", "users.read"},
	"room.member_leave":        {"rooms.list", "users.read"},
	"room.settings_changed":    {"rooms.list"},
	"file.uploaded":            {"files.upload"},
	"search.after_query":       {"messages.read"},
	"notification.before_send": {"messages.read"},
	"notification.after_send":  {"messages.read"},
	"auth.post_login":          {"users.read"},
	"auth.post_register":       {"users.read"},
	"user.status_change":       {"users.presence"},
	"user.profile_update":      {"users.read"},
}

// CheckPermission verifies that an app has the required permissions for an event.
func (s *Sandbox) CheckPermission(adapter *AppAdapter, eventName string) error {
	requiredPerms, exists := EventPermissionRequirements[eventName]
	if !exists {
		// No specific permissions required for this event
		return nil
	}

	for _, required := range requiredPerms {
		if !hasPermission(adapter.Permissions, required) {
			return fmt.Errorf("app '%s' missing required permission '%s' for event '%s'",
				adapter.Name, required, eventName)
		}
	}

	return nil
}

// ExecuteWithTimeout runs a function with a timeout enforced by the sandbox.
func (s *Sandbox) ExecuteWithTimeout(ctx context.Context, appName string, fn func(ctx context.Context) (interface{}, error)) (interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, s.ExecutionTimeout)
	defer cancel()

	type result struct {
		value interface{}
		err   error
	}

	ch := make(chan result, 1)
	go func() {
		v, err := fn(ctx)
		ch <- result{v, err}
	}()

	select {
	case res := <-ch:
		return res.value, res.err
	case <-ctx.Done():
		log.Printf("[Sandbox] App '%s' execution timed out after %v", appName, s.ExecutionTimeout)
		return nil, fmt.Errorf("app '%s' execution timed out", appName)
	}
}

// ValidateHTTPDomain checks if an outgoing HTTP request domain is allowed.
func (s *Sandbox) ValidateHTTPDomain(adapter *AppAdapter, domain string) error {
	if !hasPermission(adapter.Permissions, "http.outgoing") {
		return fmt.Errorf("app does not have 'http.outgoing' permission")
	}

	// Block internal/private network access
	blockedPrefixes := []string{
		"localhost",
		"127.0.0.1",
		"10.",
		"172.16.", "172.17.", "172.18.", "172.19.",
		"172.20.", "172.21.", "172.22.", "172.23.",
		"172.24.", "172.25.", "172.26.", "172.27.",
		"172.28.", "172.29.", "172.30.", "172.31.",
		"192.168.",
		"169.254.",
		"0.0.0.0",
	}

	lowerDomain := strings.ToLower(domain)
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(lowerDomain, prefix) {
			return fmt.Errorf("access to internal network address '%s' is blocked", domain)
		}
	}

	return nil
}

func hasPermission(perms []string, required string) bool {
	for _, p := range perms {
		if p == required {
			return true
		}
	}
	return false
}
