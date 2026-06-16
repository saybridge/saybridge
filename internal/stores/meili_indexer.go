package stores

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/meilisearch/meilisearch-go"
	"github.com/nats-io/nats.go"
	"github.com/saybridge/saybridge/internal/domain"
)

type meilisearchRepository struct {
	client meilisearch.ServiceManager
}

// NewSearchRepository instantiates a new Meilisearch-backed domain.SearchRepository.
func NewSearchRepository(client meilisearch.ServiceManager) domain.SearchRepository {
	return &meilisearchRepository{client: client}
}

// MessageDocument represents the structure saved in Meilisearch.
type MessageDocument struct {
	MessageID  string            `json:"message_id"`
	RoomID     string            `json:"room_id"`
	TenantID   string            `json:"tenant_id"`
	SenderID   string            `json:"sender_id"`
	SenderName string            `json:"sender_name"`
	Content    string            `json:"content"`
	MsgType    string            `json:"msg_type"`
	IsEdited   bool              `json:"is_edited"`
	IsDeleted  bool              `json:"is_deleted"`
	Reactions  map[string]string `json:"reactions,omitempty"`
	CreatedAt  int64             `json:"created_at"` // Unix timestamp
}

func (r *meilisearchRepository) IndexMessage(ctx context.Context, tenantID string, msg *domain.Message) error {
	doc := MessageDocument{
		MessageID:  msg.MessageID,
		RoomID:     msg.RoomID,
		TenantID:   tenantID,
		SenderID:   msg.SenderID,
		SenderName: msg.SenderName,
		Content:    msg.Content,
		MsgType:    msg.MsgType,
		IsEdited:   msg.IsEdited,
		IsDeleted:  msg.IsDeleted,
		Reactions:  msg.Reactions,
		CreatedAt:  msg.CreatedAt.Unix(),
	}

	_, err := r.client.Index("messages").AddDocuments(doc)
	return err
}

func (r *meilisearchRepository) SearchMessages(ctx context.Context, tenantID string, roomIDs []string, query string, limit int) ([]domain.Message, error) {
	if len(roomIDs) == 0 {
		return []domain.Message{}, nil
	}

	var roomFilters []string
	for _, rid := range roomIDs {
		roomFilters = append(roomFilters, fmt.Sprintf("room_id = '%s'", rid))
	}
	roomFilterStr := strings.Join(roomFilters, " OR ")

	// Filter by tenant and authorized room memberships
	filter := fmt.Sprintf("tenant_id = '%s' AND (%s)", tenantID, roomFilterStr)

	resp, err := r.client.Index("messages").Search(query, &meilisearch.SearchRequest{
		Limit:  int64(limit),
		Filter: filter,
	})
	if err != nil {
		return nil, err
	}

	var messages []domain.Message
	for _, hit := range resp.Hits {
		hitBytes, err := json.Marshal(hit)
		if err != nil {
			continue
		}
		var doc MessageDocument
		if err := json.Unmarshal(hitBytes, &doc); err != nil {
			continue
		}

		messages = append(messages, domain.Message{
			RoomID:     doc.RoomID,
			MessageID:  doc.MessageID,
			SenderID:   doc.SenderID,
			SenderName: doc.SenderName,
			Content:    doc.Content,
			MsgType:    doc.MsgType,
			IsEdited:   doc.IsEdited,
			IsDeleted:  doc.IsDeleted,
			Reactions:  doc.Reactions,
			CreatedAt:  time.Unix(doc.CreatedAt, 0),
		})
	}

	return messages, nil
}

// StartSearchIndexerWorker runs a durable JetStream queue group subscription to index messages in Meilisearch.
func StartSearchIndexerWorker(js nats.JetStreamContext, repo domain.SearchRepository) error {
	log.Info().Msg("[Indexer] Starting background NATS JetStream Search Indexer Worker...")

	_, err := js.QueueSubscribe("tenant.*.room.*", "search_indexer_group", func(m *nats.Msg) {
		defer m.Ack() // Manual ACK once processed

		var payload struct {
			Event  string         `json:"event"`
			RoomID string         `json:"room_id"`
			Data   domain.Message `json:"data"`
		}

		if err := json.Unmarshal(m.Data, &payload); err != nil {
			log.Error().Err(err).Msg("[Indexer] Error parsing message payload")
			return
		}

		// Only index message receive and update events
		if payload.Event != "msg:receive" {
			return
		}

		// Extract tenantID from subject: tenant.<tenantID>.room.<roomID>
		parts := strings.Split(m.Subject, ".")
		if len(parts) < 2 {
			log.Warn().Msgf("[Indexer] Invalid subject format: %s", m.Subject)
			return
		}
		tenantID := parts[1]

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := repo.IndexMessage(ctx, tenantID, &payload.Data); err != nil {
			log.Error().Err(err).Msgf("[Indexer] Failed to index message [%s]", payload.Data.MessageID)
		} else {
			log.Info().Msgf("[Indexer] Successfully indexed message [%s] for room [%s]", payload.Data.MessageID, payload.RoomID)
		}
	}, nats.Durable("search_indexer"), nats.ManualAck())

	return err
}
