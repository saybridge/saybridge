package notification

import (
	"context"
	"time"

	natspkg "github.com/nats-io/nats.go"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/events"
)

type DesktopTransport struct {
	nc *natspkg.Conn
}

// NewDesktopTransport builds a transport that fans notifications out to the
// recipient's connected clients over core NATS. Notifications are ephemeral
// real-time events, so they are published on the core bus (like presence and
// system events) rather than persisted to a JetStream stream.
func NewDesktopTransport(nc *natspkg.Conn) *DesktopTransport {
	return &DesktopTransport{nc: nc}
}

func (t *DesktopTransport) Name() string {
	return "desktop"
}

func (t *DesktopTransport) Send(ctx context.Context, userID string, notification Notification) error {
	subject := events.NotificationSubject(domain.DefaultTenantID, userID)

	payload := map[string]interface{}{
		"event": "notification",
		"data": map[string]interface{}{
			"type":       notification.Type,
			"title":      notification.Title,
			"message":    notification.Body,
			"priority":   notification.Priority,
			"room_id":    notification.RoomID,
			"extra_data": notification.Data,
			"created_at": time.Now(),
		},
	}

	return events.PublishJSONCore(t.nc, subject, payload)
}
