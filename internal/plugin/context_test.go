package plugin

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestContextFrom_ExtractsTypedDeps(t *testing.T) {
	api := &gin.RouterGroup{}
	proxy := NewPluginProxyRouter()
	called := false
	sendFn := func(ctx context.Context, senderID, senderName, roomID, content string) (string, error) {
		called = true
		return "msg-1", nil
	}

	payload := map[string]interface{}{
		"api":             api,
		"proxy":           proxy,
		"send_message_fn": sendFn,
	}

	pc := ContextFrom(payload)

	if pc.API != api {
		t.Error("API not extracted")
	}
	if pc.Proxy != proxy {
		t.Error("Proxy not extracted")
	}
	if pc.SendMessageFn == nil {
		t.Fatal("SendMessageFn not extracted")
	}
	if _, err := pc.SendMessageFn(context.Background(), "u1", "U1", "r1", "hi"); err != nil {
		t.Fatalf("SendMessageFn returned error: %v", err)
	}
	if !called {
		t.Error("SendMessageFn was not the wired closure")
	}
}

func TestContextFrom_MissingKeysAreNil(t *testing.T) {
	// Missing or mistyped keys must yield zero values, not panic.
	pc := ContextFrom(map[string]interface{}{
		"db": "not-a-db", // wrong type → must be ignored
	})
	if pc.DB != nil {
		t.Error("mistyped DB should be nil")
	}
	if pc.RDB != nil || pc.API != nil || pc.SendMessageFn != nil {
		t.Error("absent keys should be nil")
	}
}
