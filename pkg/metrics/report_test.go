package metrics

import "testing"

func TestGatherSnapshot(t *testing.T) {
	IncMessage("text")
	IncMessage("file")
	IncAuth("success")
	IncNotification("message", "desktop", "sent")
	ObserveAI("openai", "gpt-4o", "success", 0, 100, 50)

	snap := Gather()

	if snap.Messages.Total < 2 {
		t.Errorf("messages total = %v, want >= 2", snap.Messages.Total)
	}
	if snap.Messages.ByType["text"] < 1 {
		t.Errorf("messages by type[text] = %v, want >= 1", snap.Messages.ByType["text"])
	}
	if snap.Auth["success"] < 1 {
		t.Errorf("auth[success] = %v, want >= 1", snap.Auth["success"])
	}
	if snap.Notifications.ByOutcome["sent"] < 1 {
		t.Errorf("notifications[sent] = %v, want >= 1", snap.Notifications.ByOutcome["sent"])
	}
	if snap.AI.InputTokens < 100 {
		t.Errorf("ai input tokens = %v, want >= 100", snap.AI.InputTokens)
	}
	if snap.AI.Requests < 1 {
		t.Errorf("ai requests = %v, want >= 1", snap.AI.Requests)
	}
	// Runtime metrics from the Go collector should always be present.
	if snap.Runtime.Goroutines <= 0 {
		t.Errorf("runtime goroutines = %v, want > 0", snap.Runtime.Goroutines)
	}
}
