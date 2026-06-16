package events

import (
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
)

// RoomSubject returns the canonical NATS subject for a room.
func RoomSubject(tenantID, roomID string) string {
	return fmt.Sprintf("tenant.%s.room.%s", tenantID, roomID)
}

// PresenceSubject returns the NATS subject for tenant presence events.
func PresenceSubject(tenantID string) string {
	return fmt.Sprintf("tenant.%s.presence", tenantID)
}

// NotificationSubject returns the NATS subject for user notifications.
func NotificationSubject(tenantID, userID string) string {
	return fmt.Sprintf("tenant.%s.notifications.%s", tenantID, userID)
}

// SystemSubject returns the NATS subject for system-wide events.
func SystemSubject(tenantID string) string {
	return fmt.Sprintf("tenant.%s.system", tenantID)
}

// PublishJSON marshals data to JSON and publishes to the given NATS subject.
func PublishJSON(js nats.JetStreamContext, subject string, data interface{}) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal event payload: %w", err)
	}
	_, err = js.Publish(subject, payload)
	if err != nil {
		return fmt.Errorf("failed to publish to %s: %w", subject, err)
	}
	return nil
}
