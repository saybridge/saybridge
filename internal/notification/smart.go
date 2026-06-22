package notification

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Notification priority levels, ordered low → urgent.
const (
	PriorityLow       = "low"
	PriorityNormal    = "normal"
	PriorityImportant = "important"
	PriorityUrgent    = "urgent"
)

// groupWindow is how long consecutive messages to the same recipient in the same
// room are collapsed into a single notification group.
const groupWindow = 30 * time.Second

// groupKeyTTL bounds how long grouping state lives in Redis. A few multiples of
// the window is plenty; state naturally resets once a window lapses.
const groupKeyTTL = 5 * time.Minute

// SmartFilter applies notification priority classification, focus-mode
// suppression, and notification grouping.
//
// This logic previously lived in the `smart-notifications` WASM plugin. It was
// moved into core because every deployment needs it and the plugin boundary
// added no value — it could not be meaningfully toggled off without breaking
// notification quality.
type SmartFilter struct {
	rdb *redis.Client
}

// NewSmartFilter builds a SmartFilter. rdb may be nil, in which case VIP lookups
// and grouping are disabled (every notification is delivered immediately).
func NewSmartFilter(rdb *redis.Client) *SmartFilter {
	return &SmartFilter{rdb: rdb}
}

// ClassifyPriority derives a notification priority from message context.
//
//   - system / bot messages          → low
//   - @channel / @here broadcasts     → important
//   - direct messages or @mentions    → urgent
//   - messages from a VIP sender      → urgent
//   - everything else                 → normal
func ClassifyPriority(content, roomType, recipientID, senderID string, isVIP bool) string {
	if senderID == "system" ||
		strings.HasPrefix(content, "🤖") ||
		strings.HasPrefix(content, "📝") ||
		strings.HasPrefix(content, "🌐") {
		return PriorityLow
	}
	if strings.Contains(content, "@channel") || strings.Contains(content, "@here") {
		return PriorityImportant
	}
	if roomType == "direct" || strings.Contains(content, recipientID) {
		return PriorityUrgent
	}
	if isVIP {
		return PriorityUrgent
	}
	return PriorityNormal
}

// IsFocusMode reports whether the recipient is currently suppressing low-signal
// notifications, based on their presence and custom status text.
func IsFocusMode(presence, customStatus string) bool {
	if presence == "busy" || presence == "dnd" {
		return true
	}
	return strings.Contains(customStatus, "🔇 Focus") || strings.Contains(customStatus, "🌙 Sleeping")
}

// IsVIP reports whether senderID is marked as a VIP for recipientID. VIP markers
// are set out-of-band under the Redis key "vip:<recipient>:<sender>".
func (f *SmartFilter) IsVIP(ctx context.Context, recipientID, senderID string) bool {
	if f.rdb == nil || senderID == "" {
		return false
	}
	key := fmt.Sprintf("vip:%s:%s", recipientID, senderID)
	v, _ := f.rdb.Get(ctx, key).Result()
	return v == "true"
}

// GroupDecision is the outcome of grouping: whether to deliver now and, if so,
// the (possibly summarized) body to deliver.
type GroupDecision struct {
	Deliver bool
	Body    string
}

// Group collapses bursts of messages to the same recipient in the same room.
// The first message in a window delivers immediately; subsequent messages within
// the window are suppressed, and a rolled-up summary ("X, Y sent N new messages")
// is delivered on every third message.
//
// When no Redis client is configured, grouping is a no-op and every message is
// delivered with a simple body.
func (f *SmartFilter) Group(ctx context.Context, recipientID, roomID, senderName string) GroupDecision {
	if senderName == "" {
		senderName = "Someone"
	}
	if f.rdb == nil {
		return GroupDecision{Deliver: true, Body: senderName + " sent a new message"}
	}

	lastKey := "notif:last_time:" + recipientID + ":" + roomID
	countKey := "notif:count:" + recipientID + ":" + roomID
	sendersKey := "notif:senders:" + recipientID + ":" + roomID

	now := time.Now().Unix()
	lastStr, _ := f.rdb.Get(ctx, lastKey).Result()
	last, _ := strconv.ParseInt(lastStr, 10, 64)

	// Window expired or first message → start a fresh group and deliver now.
	if last == 0 || now-last >= int64(groupWindow.Seconds()) {
		f.rdb.Set(ctx, countKey, 1, groupKeyTTL)
		f.rdb.Set(ctx, sendersKey, senderName, groupKeyTTL)
		f.rdb.Set(ctx, lastKey, now, groupKeyTTL)
		return GroupDecision{Deliver: true, Body: senderName + " sent a new message"}
	}

	// Still inside the window → accumulate.
	count, _ := f.rdb.Incr(ctx, countKey).Result()
	f.rdb.Expire(ctx, countKey, groupKeyTTL)

	senders, _ := f.rdb.Get(ctx, sendersKey).Result()
	switch {
	case senders == "":
		senders = senderName
	case !strings.Contains(senders, senderName):
		senders = senders + ", " + senderName
	}
	f.rdb.Set(ctx, sendersKey, senders, groupKeyTTL)

	if count%3 == 0 {
		return GroupDecision{
			Deliver: true,
			Body:    fmt.Sprintf("%s sent %d new messages", senders, count),
		}
	}
	return GroupDecision{Deliver: false}
}
