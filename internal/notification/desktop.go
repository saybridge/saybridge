package notification

import (
	"context"
	"time"

	natspkg "github.com/nats-io/nats.go"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/events"
)

type DesktopTransport struct {
	js natspkg.JetStreamContext
}

func NewDesktopTransport(js natspkg.JetStreamContext) *DesktopTransport {
	return &DesktopTransport{js: js}
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
			"room_id":    notification.RoomID,
			"extra_data": notification.Data,
			"created_at": time.Now(),
		},
	}

	return events.PublishJSON(t.js, subject, payload)
}
