// Package natsbus provides NATS + JetStream connection initialization.
package natsbus

import (
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/nats-io/nats.go"
	"github.com/saybridge/saybridge/pkg/config"
)

// NewConnection establishes connection to the NATS broker, boots JetStream, and asserts stream existence.
func NewConnection(cfg *config.Config) (nats.JetStreamContext, *nats.Conn, error) {
	nc, err := nats.Connect(cfg.NatsURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to establish NATS connection: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("failed to initialize NATS JetStream context: %w", err)
	}

	// Bootstrap stream for persistent messaging delivery
	streamName := "chat_messages"
	subjects := []string{"tenant.*.room.*"}

	_, err = js.StreamInfo(streamName)
	if err != nil {
		_, err = js.AddStream(&nats.StreamConfig{
			Name:     streamName,
			Subjects: subjects,
			Storage:  nats.FileStorage,
		})
		if err != nil {
			nc.Close()
			return nil, nil, fmt.Errorf("failed to bootstrap NATS stream [%s]: %w", streamName, err)
		}
		log.Info().Msgf("[Infra] Bootstrapped durable NATS Stream [%s] with subjects %v", streamName, subjects)
	}

	log.Info().Msgf("[Infra] NATS connection to [%s] established (JetStream Active)", cfg.NatsURL)
	return js, nc, nil
}
