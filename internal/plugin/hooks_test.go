package plugin

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/saybridge/saybridge/internal/domain"
)

func TestHookRegistry_OnAndEmit(t *testing.T) {
	reg := NewHookRegistry()

	var callCount int32

	reg.On(PreLogin, HookHandler{
		Name:     "test-handler",
		Priority: 10,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			atomic.AddInt32(&callCount, 1)
			return nil, nil
		},
	})

	ctx := context.Background()
	err := reg.Emit(ctx, PreLogin, map[string]interface{}{"user_id": "user-1"})
	if err != nil {
		t.Fatalf("Emit returned unexpected error: %v", err)
	}

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected handler to be called once, got %d", callCount)
	}
}

func TestHookRegistry_PriorityOrder(t *testing.T) {
	reg := NewHookRegistry()

	var order []string

	reg.On(BeforeSendMessage, HookHandler{
		Name:     "low-priority",
		Priority: 50,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			order = append(order, "low")
			return nil, nil
		},
	})

	reg.On(BeforeSendMessage, HookHandler{
		Name:     "high-priority",
		Priority: 10,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			order = append(order, "high")
			return nil, nil
		},
	})

	reg.On(BeforeSendMessage, HookHandler{
		Name:     "medium-priority",
		Priority: 30,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			order = append(order, "medium")
			return nil, nil
		},
	})

	ctx := context.Background()
	err := reg.Emit(ctx, BeforeSendMessage, map[string]interface{}{"content": "test"})
	if err != nil {
		t.Fatalf("Emit returned unexpected error: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("Expected 3 handlers to run, got %d", len(order))
	}

	expected := []string{"high", "medium", "low"}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("Position %d: expected '%s', got '%s'", i, v, order[i])
		}
	}
}

func TestHookRegistry_Emit_FailFast(t *testing.T) {
	reg := NewHookRegistry()

	var secondCalled bool

	reg.On(PreRegister, HookHandler{
		Name:     "failing-handler",
		Priority: 10,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			return nil, errors.New("validation failed")
		},
	})

	reg.On(PreRegister, HookHandler{
		Name:     "second-handler",
		Priority: 20,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			secondCalled = true
			return nil, nil
		},
	})

	ctx := context.Background()
	err := reg.Emit(ctx, PreRegister, map[string]interface{}{"email": "test@example.com"})
	if err == nil {
		t.Fatal("Expected error from Emit, got nil")
	}

	if secondCalled {
		t.Error("Second handler should NOT have been called after first handler error (fail-fast)")
	}
}

func TestHookRegistry_EmitTyped_Cancel(t *testing.T) {
	reg := NewHookRegistry()

	reg.OnTyped(BeforeCreateRoom, TypedHookHandler{
		Name:     "room-quota-check",
		Priority: 10,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload interface{}) (*HookResult, error) {
			return &HookResult{
				Cancel:       true,
				CancelReason: "tenant quota exceeded",
			}, nil
		},
	})

	ctx := context.Background()
	_, cancelled, err := reg.EmitTyped(ctx, BeforeCreateRoom, map[string]string{
		"creator_id": "user-1",
		"name":       "overflow-room",
	})

	if err != nil {
		t.Fatalf("EmitTyped returned unexpected error: %v", err)
	}

	if !cancelled {
		t.Error("Expected operation to be cancelled, but it was not")
	}
}

func TestHookRegistry_EmitTyped_StopPropagation(t *testing.T) {
	reg := NewHookRegistry()

	var secondCalled bool

	reg.OnTyped(AfterSendMessage, TypedHookHandler{
		Name:     "first-handler",
		Priority: 10,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload interface{}) (*HookResult, error) {
			return &HookResult{
				StopPropagation: true,
			}, nil
		},
	})

	reg.OnTyped(AfterSendMessage, TypedHookHandler{
		Name:     "second-handler",
		Priority: 20,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload interface{}) (*HookResult, error) {
			secondCalled = true
			return nil, nil
		},
	})

	ctx := context.Background()
	_, cancelled, err := reg.EmitTyped(ctx, AfterSendMessage, map[string]string{"message_id": "msg-1"})
	if err != nil {
		t.Fatalf("EmitTyped returned unexpected error: %v", err)
	}

	if cancelled {
		t.Error("StopPropagation should not cancel the operation")
	}

	if secondCalled {
		t.Error("Second handler should NOT have been called after StopPropagation")
	}
}

func TestHookRegistry_EmitTyped_Mutation(t *testing.T) {
	reg := NewHookRegistry()

	type MessagePayload struct {
		Content string
	}

	reg.OnTyped(BeforeSendMessage, TypedHookHandler{
		Name:     "content-filter",
		Priority: 10,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload interface{}) (*HookResult, error) {
			p := payload.(*MessagePayload)
			return &HookResult{
				Data: &MessagePayload{Content: p.Content + " [filtered]"},
			}, nil
		},
	})

	ctx := context.Background()
	result, cancelled, err := reg.EmitTyped(ctx, BeforeSendMessage, &MessagePayload{Content: "hello"})
	if err != nil {
		t.Fatalf("EmitTyped returned unexpected error: %v", err)
	}
	if cancelled {
		t.Error("Expected operation to not be cancelled")
	}

	mutated := result.(*MessagePayload)
	expected := "hello [filtered]"
	if mutated.Content != expected {
		t.Errorf("Expected mutated content '%s', got '%s'", expected, mutated.Content)
	}
}

func TestHookRegistry_RemoveHandler(t *testing.T) {
	reg := NewHookRegistry()

	// Register untyped handler
	reg.On(PostLogin, HookHandler{
		Name:     "analytics-tracker",
		Priority: 10,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
	})

	// Register typed handler with same name
	reg.OnTyped(PostLogin, TypedHookHandler{
		Name:     "analytics-tracker",
		Priority: 10,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload interface{}) (*HookResult, error) {
			return nil, nil
		},
	})

	if !reg.HasHandlers(PostLogin) {
		t.Fatal("Expected handlers to be registered for PostLogin")
	}
	if reg.HandlerCount(PostLogin) != 1 {
		t.Errorf("Expected 1 untyped handler, got %d", reg.HandlerCount(PostLogin))
	}

	// Remove all handlers with name "analytics-tracker"
	reg.RemoveHandler(PostLogin, "analytics-tracker")

	if reg.HasHandlers(PostLogin) {
		t.Error("Expected no untyped handlers after removal")
	}
	if reg.HandlerCount(PostLogin) != 0 {
		t.Errorf("Expected 0 untyped handlers after removal, got %d", reg.HandlerCount(PostLogin))
	}
}

func TestHookRegistry_RemoveAllHandlers(t *testing.T) {
	reg := NewHookRegistry()

	pluginName := "test-plugin"

	reg.On(PreLogin, HookHandler{Name: pluginName, Priority: 10, Runtime: RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) { return nil, nil }})
	reg.On(PostLogin, HookHandler{Name: pluginName, Priority: 10, Runtime: RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) { return nil, nil }})
	reg.OnTyped(BeforeSendMessage, TypedHookHandler{Name: pluginName, Priority: 10, Runtime: RuntimeNative,
		Fn: func(ctx context.Context, payload interface{}) (*HookResult, error) { return nil, nil }})

	// Keep another plugin's handler
	reg.On(PreLogin, HookHandler{Name: "other-plugin", Priority: 20, Runtime: RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) { return nil, nil }})

	reg.RemoveAllHandlers(pluginName)

	if reg.HandlerCount(PreLogin) != 1 {
		t.Errorf("Expected 1 remaining handler for PreLogin (other-plugin), got %d", reg.HandlerCount(PreLogin))
	}
	if reg.HandlerCount(PostLogin) != 0 {
		t.Errorf("Expected 0 handlers for PostLogin after removal, got %d", reg.HandlerCount(PostLogin))
	}
}

func TestHookRegistry_EmitCollect(t *testing.T) {
	reg := NewHookRegistry()

	reg.On(AuthenticateExternal, HookHandler{
		Name:     "ldap-provider",
		Priority: 10,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			return nil, errors.New("LDAP server unavailable")
		},
	})

	reg.On(AuthenticateExternal, HookHandler{
		Name:     "saml-provider",
		Priority: 20,
		Runtime:  RuntimeNative,
		Fn: func(ctx context.Context, payload map[string]interface{}) (interface{}, error) {
			return &domain.User{
				BaseModel: domain.BaseModel{ID: "external-user-1"},
				Username:  "saml_user",
			}, nil
		},
	})

	ctx := context.Background()
	result, err := reg.EmitCollect(ctx, AuthenticateExternal, map[string]interface{}{
		"email":    "user@corp.com",
		"password": "pass",
	})

	if err != nil {
		t.Fatalf("EmitCollect returned unexpected error: %v", err)
	}

	user, ok := result.(*domain.User)
	if !ok || user == nil {
		t.Fatal("Expected *domain.User result from EmitCollect")
	}
	if user.Username != "saml_user" {
		t.Errorf("Expected username 'saml_user', got '%s'", user.Username)
	}
}

func TestHookRegistry_EmitNoHandlers(t *testing.T) {
	reg := NewHookRegistry()
	ctx := context.Background()

	err := reg.Emit(ctx, OnCronTick, map[string]interface{}{})
	if err != nil {
		t.Errorf("Emit with no handlers should return nil, got: %v", err)
	}

	result, cancelled, err := reg.EmitTyped(ctx, OnCronTick, nil)
	if err != nil {
		t.Errorf("EmitTyped with no handlers should return nil error, got: %v", err)
	}
	if cancelled {
		t.Error("EmitTyped with no handlers should not cancel")
	}
	if result != nil {
		t.Errorf("EmitTyped with no handlers should return original payload (nil), got: %v", result)
	}
}
