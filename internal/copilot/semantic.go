package copilot

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ── Semantic Search with pgvector ─────────────────────────────────────────────

// MessageEmbedding represents a vector-indexed message for semantic search.
type MessageEmbedding struct {
	MessageID string    `gorm:"primaryKey;type:uuid" json:"message_id"`
	RoomID    string    `gorm:"type:uuid;index" json:"room_id"`
	Content   string    `gorm:"type:text" json:"content"`
	Embedding string    `gorm:"type:text" json:"-"` // stored as pgvector string representation
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (MessageEmbedding) TableName() string { return "message_embeddings" }

// SemanticIndex manages vector embeddings for semantic message search.
type SemanticIndex struct {
	db      *gorm.DB
	gateway *Gateway
	mu      sync.Mutex
	batch   []pendingEmbed
	ticker  *time.Ticker
}

type pendingEmbed struct {
	MessageID string
	RoomID    string
	Content   string
}

// SemanticSearchResult represents a single search result with relevance score.
type SemanticSearchResult struct {
	MessageID string  `json:"message_id"`
	RoomID    string  `json:"room_id"`
	Content   string  `json:"content"`
	Score     float64 `json:"score"`
}

// NewSemanticIndex creates a new semantic index manager.
func NewSemanticIndex(db *gorm.DB, gw *Gateway) *SemanticIndex {
	si := &SemanticIndex{
		db:      db,
		gateway: gw,
		batch:   make([]pendingEmbed, 0, 100),
	}
	return si
}

// EnsureSchema creates the pgvector extension and message_embeddings table.
func (si *SemanticIndex) EnsureSchema() error {
	// Enable pgvector extension (requires superuser or extension installed)
	if err := si.db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		log.Printf("[SemanticIndex] Warning: could not create vector extension: %v", err)
		// Non-fatal — extension may already exist or we may not have permissions
	}

	// Create table with vector column
	createSQL := `
		CREATE TABLE IF NOT EXISTS message_embeddings (
			message_id UUID PRIMARY KEY,
			room_id UUID NOT NULL,
			content TEXT NOT NULL,
			embedding vector(1536) NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`
	if err := si.db.Exec(createSQL).Error; err != nil {
		return fmt.Errorf("create message_embeddings table: %w", err)
	}

	// Create indexes
	si.db.Exec("CREATE INDEX IF NOT EXISTS idx_embeddings_room ON message_embeddings(room_id)")
	// IVFFlat index for fast approximate nearest neighbor search
	si.db.Exec("CREATE INDEX IF NOT EXISTS idx_embeddings_vector ON message_embeddings USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)")

	log.Printf("[SemanticIndex] ✓ Schema ensured (pgvector message_embeddings)")
	return nil
}

// IndexMessage queues a message for embedding and storage.
func (si *SemanticIndex) IndexMessage(messageID, roomID, content string) {
	if content == "" || strings.HasPrefix(content, "/") {
		return
	}

	si.mu.Lock()
	si.batch = append(si.batch, pendingEmbed{
		MessageID: messageID,
		RoomID:    roomID,
		Content:   content,
	})
	batchSize := len(si.batch)
	si.mu.Unlock()

	// Flush immediately if batch is large enough
	if batchSize >= 50 {
		go si.flushBatch()
	}
}

// StartBatchProcessor starts a background goroutine that flushes the batch every 5 seconds.
func (si *SemanticIndex) StartBatchProcessor(ctx context.Context) {
	si.ticker = time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-ctx.Done():
				si.ticker.Stop()
				si.flushBatch() // final flush
				return
			case <-si.ticker.C:
				si.flushBatch()
			}
		}
	}()
	log.Printf("[SemanticIndex] ✓ Batch processor started (flush every 5s)")
}

func (si *SemanticIndex) flushBatch() {
	si.mu.Lock()
	if len(si.batch) == 0 {
		si.mu.Unlock()
		return
	}
	items := make([]pendingEmbed, len(si.batch))
	copy(items, si.batch)
	si.batch = si.batch[:0]
	si.mu.Unlock()

	// Extract texts for embedding
	texts := make([]string, len(items))
	for i, item := range items {
		texts[i] = item.Content
	}

	// Generate embeddings via AI Gateway
	embeddings, err := si.gateway.Embed(context.Background(), texts)
	if err != nil {
		log.Printf("[SemanticIndex] Failed to generate embeddings: %v", err)
		return
	}

	if len(embeddings) != len(items) {
		log.Printf("[SemanticIndex] Embedding count mismatch: got %d, expected %d", len(embeddings), len(items))
		return
	}

	// Insert into database
	for i, item := range items {
		vecStr := vectorToString(embeddings[i])
		sql := `INSERT INTO message_embeddings (message_id, room_id, content, embedding)
				VALUES (?, ?, ?, ?::vector)
				ON CONFLICT (message_id) DO NOTHING`
		if err := si.db.Exec(sql, item.MessageID, item.RoomID, item.Content, vecStr).Error; err != nil {
			log.Printf("[SemanticIndex] Failed to index message %s: %v", item.MessageID, err)
		}
	}

	log.Printf("[SemanticIndex] Indexed %d messages", len(items))
}

// Search performs a semantic search across messages using vector similarity.
func (si *SemanticIndex) Search(ctx context.Context, query string, roomIDs []string, limit int) ([]SemanticSearchResult, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	// Generate query embedding
	embeddings, err := si.gateway.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, fmt.Errorf("empty embedding result")
	}

	queryVec := vectorToString(embeddings[0])

	var results []SemanticSearchResult

	if len(roomIDs) > 0 {
		sql := `SELECT message_id, room_id, content, 1 - (embedding <=> ?::vector) AS score
				FROM message_embeddings
				WHERE room_id = ANY(?)
				ORDER BY embedding <=> ?::vector
				LIMIT ?`
		if err := si.db.WithContext(ctx).Raw(sql, queryVec, roomIDs, queryVec, limit).Scan(&results).Error; err != nil {
			return nil, fmt.Errorf("semantic search: %w", err)
		}
	} else {
		sql := `SELECT message_id, room_id, content, 1 - (embedding <=> ?::vector) AS score
				FROM message_embeddings
				ORDER BY embedding <=> ?::vector
				LIMIT ?`
		if err := si.db.WithContext(ctx).Raw(sql, queryVec, queryVec, limit).Scan(&results).Error; err != nil {
			return nil, fmt.Errorf("semantic search: %w", err)
		}
	}

	return results, nil
}

// vectorToString converts a float32 slice to pgvector string format: [0.1,0.2,0.3]
func vectorToString(vec []float32) string {
	if len(vec) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			sb.WriteByte(',')
		}
		// Use 8 decimal places, avoid scientific notation for small values
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			sb.WriteString("0")
		} else {
			sb.WriteString(fmt.Sprintf("%.8f", v))
		}
	}
	sb.WriteByte(']')
	return sb.String()
}
