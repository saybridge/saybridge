package notification

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestClassifyPriority(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		roomType  string
		recipient string
		sender    string
		isVIP     bool
		want      string
	}{
		{"system message", "deploy finished", "channel", "u1", "system", false, PriorityLow},
		{"bot prefix", "🤖 build ok", "channel", "u1", "u2", false, PriorityLow},
		{"channel broadcast", "hey @channel ship it", "channel", "u1", "u2", false, PriorityImportant},
		{"here broadcast", "@here standup", "group", "u1", "u2", false, PriorityImportant},
		{"direct message", "hi", "direct", "u1", "u2", false, PriorityUrgent},
		{"mention by id", "ping u1 please", "channel", "u1", "u2", false, PriorityUrgent},
		{"vip sender", "fyi", "channel", "u1", "u2", true, PriorityUrgent},
		{"plain channel message", "hello team", "channel", "u1", "u2", false, PriorityNormal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyPriority(tc.content, tc.roomType, tc.recipient, tc.sender, tc.isVIP)
			if got != tc.want {
				t.Fatalf("ClassifyPriority = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsFocusMode(t *testing.T) {
	tests := []struct {
		presence string
		custom   string
		want     bool
	}{
		{"busy", "", true},
		{"dnd", "", true},
		{"online", "🔇 Focus until 5pm", true},
		{"online", "🌙 Sleeping", true},
		{"online", "🚀 Shipping", false},
		{"away", "", false},
	}

	for _, tc := range tests {
		if got := IsFocusMode(tc.presence, tc.custom); got != tc.want {
			t.Fatalf("IsFocusMode(%q,%q) = %v, want %v", tc.presence, tc.custom, got, tc.want)
		}
	}
}

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func TestGroup(t *testing.T) {
	ctx := context.Background()
	f := NewSmartFilter(newTestRedis(t))

	// First message in the window delivers immediately.
	d := f.Group(ctx, "rcpt", "room1", "Alice")
	if !d.Deliver {
		t.Fatal("first message should deliver")
	}

	// Messages 2 and 3 are within the window: 2 is suppressed, 3 delivers a summary.
	if d := f.Group(ctx, "rcpt", "room1", "Bob"); d.Deliver {
		t.Fatal("second message should be grouped (suppressed)")
	}
	d = f.Group(ctx, "rcpt", "room1", "Alice")
	if !d.Deliver {
		t.Fatal("third message should deliver a summary")
	}
	if d.Body == "" {
		t.Fatal("summary body should be non-empty")
	}
}

func TestGroupNilRedisAlwaysDelivers(t *testing.T) {
	f := NewSmartFilter(nil)
	for i := 0; i < 5; i++ {
		if d := f.Group(context.Background(), "rcpt", "room1", "Alice"); !d.Deliver {
			t.Fatalf("nil redis should always deliver (iteration %d)", i)
		}
	}
}
