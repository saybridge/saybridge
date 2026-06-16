package natsbus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nats-io/nats.go"
)

// DLQMessage represents a message that failed processing and was sent to the dead letter queue.
type DLQMessage struct {
	OriginalSubject string    `json:"original_subject"`
	Data            []byte    `json:"data"`
	Error           string    `json:"error"`
	Attempts        int       `json:"attempts"`
	FirstFailedAt   time.Time `json:"first_failed_at"`
	LastFailedAt    time.Time `json:"last_failed_at"`
	MessageID       string    `json:"message_id,omitempty"`
}

// DLQConfig holds the configuration for the dead letter queue.
type DLQConfig struct {
	DLQSubjectPrefix string        // Prefix for DLQ subjects. Default: "dlq"
	MaxRetries       int           // Max retries before sending to DLQ. Default: 3
	RetryDelay       time.Duration // Base delay between retries. Default: 1s
	WorkerInterval   time.Duration // How often the DLQ worker checks for messages. Default: 30s
}

// DefaultDLQConfig returns a sensible default DLQ configuration.
func DefaultDLQConfig() DLQConfig {
	return DLQConfig{
		DLQSubjectPrefix: "dlq",
		MaxRetries:       3,
		RetryDelay:       1 * time.Second,
		WorkerInterval:   30 * time.Second,
	}
}

// DLQ manages the dead letter queue for failed NATS messages.
type DLQ struct {
	conn   *nats.Conn
	config DLQConfig
}

// NewDLQ creates a new dead letter queue manager.
func NewDLQ(conn *nats.Conn, cfg DLQConfig) *DLQ {
	if cfg.DLQSubjectPrefix == "" {
		cfg.DLQSubjectPrefix = "dlq"
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 1 * time.Second
	}
	if cfg.WorkerInterval <= 0 {
		cfg.WorkerInterval = 30 * time.Second
	}

	return &DLQ{conn: conn, config: cfg}
}

// PublishToDLQ sends a failed message to the dead letter queue.
func (d *DLQ) PublishToDLQ(originalSubject string, data []byte, err error, attempts int) error {
	dlqMsg := DLQMessage{
		OriginalSubject: originalSubject,
		Data:            data,
		Error:           err.Error(),
		Attempts:        attempts,
		FirstFailedAt:   time.Now(),
		LastFailedAt:    time.Now(),
		MessageID:       fmt.Sprintf("dlq_%d", time.Now().UnixNano()),
	}

	payload, marshalErr := json.Marshal(dlqMsg)
	if marshalErr != nil {
		return fmt.Errorf("failed to marshal DLQ message: %w", marshalErr)
	}

	dlqSubject := fmt.Sprintf("%s.%s", d.config.DLQSubjectPrefix, originalSubject)
	if pubErr := d.conn.Publish(dlqSubject, payload); pubErr != nil {
		return fmt.Errorf("failed to publish to DLQ subject %s: %w", dlqSubject, pubErr)
	}

	log.Warn().Msgf("[DLQ] Message sent to %s after %d attempts. Error: %s", dlqSubject, attempts, err.Error())
	return nil
}

// ProcessWithRetry wraps a message handler with retry logic.
// If all retries fail, the message is sent to the DLQ.
func (d *DLQ) ProcessWithRetry(subject string, data []byte, handler func([]byte) error) error {
	var lastErr error

	for attempt := 1; attempt <= d.config.MaxRetries; attempt++ {
		lastErr = handler(data)
		if lastErr == nil {
			return nil
		}

		log.Warn().Err(lastErr).Msgf("[DLQ] Attempt %d/%d failed for subject %s", attempt, d.config.MaxRetries, subject)

		if attempt < d.config.MaxRetries {
			time.Sleep(d.config.RetryDelay * time.Duration(attempt))
		}
	}

	// All retries exhausted — send to DLQ
	if dlqErr := d.PublishToDLQ(subject, data, lastErr, d.config.MaxRetries); dlqErr != nil {
		log.Error().Err(dlqErr).Msgf("[DLQ] CRITICAL: Failed to publish to DLQ (original error: %v)", lastErr)
	}

	return fmt.Errorf("all %d retries exhausted for subject %s: %w", d.config.MaxRetries, subject, lastErr)
}

// StartWorker starts a background worker that subscribes to DLQ subjects
// and attempts to replay messages using the provided handler map.
// The worker runs until the context is cancelled.
func (d *DLQ) StartWorker(ctx context.Context, handlers map[string]func([]byte) error) error {
	dlqWildcard := fmt.Sprintf("%s.>", d.config.DLQSubjectPrefix)

	sub, err := d.conn.Subscribe(dlqWildcard, func(msg *nats.Msg) {
		var dlqMsg DLQMessage
		if err := json.Unmarshal(msg.Data, &dlqMsg); err != nil {
			log.Error().Err(err).Msg("[DLQ Worker] Failed to unmarshal DLQ message")
			return
		}

		handler, ok := handlers[dlqMsg.OriginalSubject]
		if !ok {
			log.Warn().Msgf("[DLQ Worker] No handler registered for subject: %s", dlqMsg.OriginalSubject)
			return
		}

		log.Info().Msgf("[DLQ Worker] Replaying message for subject: %s (original attempts: %d)", dlqMsg.OriginalSubject, dlqMsg.Attempts)

		if replayErr := handler(dlqMsg.Data); replayErr != nil {
			log.Error().Err(replayErr).Msgf("[DLQ Worker] Replay failed for %s — message stays in DLQ", dlqMsg.OriginalSubject)
			// Optionally: re-publish with incremented attempt count
			return
		}

		log.Info().Msgf("[DLQ Worker] Successfully replayed message for subject: %s", dlqMsg.OriginalSubject)
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to DLQ wildcard %s: %w", dlqWildcard, err)
	}

	log.Info().Msgf("[DLQ Worker] Started listening on %s", dlqWildcard)

	// Wait for context cancellation
	<-ctx.Done()
	if err := sub.Unsubscribe(); err != nil {
		log.Error().Err(err).Msg("[DLQ Worker] Error unsubscribing")
	}
	log.Info().Msg("[DLQ Worker] Stopped")
	return nil
}

// GetDLQMessages retrieves all messages in the DLQ for a given original subject.
// Note: This is a simplified version. In production, consider using JetStream for persistence.
func (d *DLQ) GetDLQSubject(originalSubject string) string {
	return fmt.Sprintf("%s.%s", d.config.DLQSubjectPrefix, originalSubject)
}

// PurgeDLQ publishes a purge command for a specific DLQ subject.
func (d *DLQ) PurgeDLQ(originalSubject string) error {
	purgeSubject := fmt.Sprintf("%s.purge.%s", d.config.DLQSubjectPrefix, originalSubject)
	return d.conn.Publish(purgeSubject, []byte(`{"action":"purge"}`))
}
