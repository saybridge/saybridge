package authz

import (
	"os"
	"testing"
)

func TestAuthzEnforcer(t *testing.T) {
	// Create temporary model and policy files for testing
	modelContent := `[request_definition]
r = sub, obj, act

[policy_definition]
p = sub_role, obj_type, act, eft

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow)) && !some(where (p.eft == deny))

[matchers]
m = g(r.sub.role, p.sub_role) && (keyMatch(r.obj.type, p.obj_type) || (r.obj.is_readonly && p.obj_type == "readonly_room")) && keyMatch(r.act, p.act)
`

	policyContent := `# Policies
p, admin, *, *, allow
p, owner, room, *, allow
p, member, room, send_message, allow
p, member, room, edit_own_message, allow
p, member, readonly_room, send_message, deny
p, guest, room, send_message, allow
`

	tmpModel, err := os.CreateTemp("", "model_*.conf")
	if err != nil {
		t.Fatalf("failed to create temp model file: %v", err)
	}
	defer os.Remove(tmpModel.Name())
	defer tmpModel.Close()

	if _, err := tmpModel.WriteString(modelContent); err != nil {
		t.Fatalf("failed to write model file: %v", err)
	}

	tmpPolicy, err := os.CreateTemp("", "policy_*.csv")
	if err != nil {
		t.Fatalf("failed to create temp policy file: %v", err)
	}
	defer os.Remove(tmpPolicy.Name())
	defer tmpPolicy.Close()

	if _, err := tmpPolicy.WriteString(policyContent); err != nil {
		t.Fatalf("failed to write policy file: %v", err)
	}

	enforcer, err := NewEnforcer(tmpModel.Name(), tmpPolicy.Name())
	if err != nil {
		t.Fatalf("failed to create enforcer: %v", err)
	}

	// Test case 1: Admin has all access
	subAdmin := Subject{Role: "admin"}
	objRoom := Object{Type: "room"}
	if !enforcer.Can(subAdmin, objRoom, "any_action") {
		t.Error("expected admin to have access to any action in room")
	}

	// Test case 2: Member can send message in normal room
	subMember := Subject{Role: "member"}
	objNormalRoom := Object{Type: "room", IsReadOnly: false}
	if !enforcer.Can(subMember, objNormalRoom, "send_message") {
		t.Error("expected member to be able to send message in normal room")
	}

	// Test case 3: Member CANNOT send message in readonly room
	objReadonlyRoom := Object{Type: "room", IsReadOnly: true}
	if enforcer.Can(subMember, objReadonlyRoom, "send_message") {
		t.Error("expected member to be denied sending message in readonly room")
	}

	// Test case 4: Member can edit own message
	if !enforcer.Can(subMember, objNormalRoom, "edit_own_message") {
		t.Error("expected member to be able to edit own message")
	}

	// Test case 5: Policy modification (Add/Remove/Get)
	err = enforcer.AddPolicy("guest", "room", "view_history", "allow")
	if err != nil {
		t.Fatalf("failed to add policy: %v", err)
	}

	subGuest := Subject{Role: "guest"}
	if !enforcer.Can(subGuest, objNormalRoom, "view_history") {
		t.Error("expected guest to be allowed to view_history after adding policy")
	}

	policies := enforcer.GetPoliciesForRole("guest")
	found := false
	for _, p := range policies {
		if p.Action == "view_history" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find view_history policy for guest")
	}

	err = enforcer.RemovePolicy("guest", "room", "view_history", "allow")
	if err != nil {
		t.Fatalf("failed to remove policy: %v", err)
	}

	if enforcer.Can(subGuest, objNormalRoom, "view_history") {
		t.Error("expected guest to be denied view_history after removing policy")
	}
}
