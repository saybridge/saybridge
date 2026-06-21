package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/saybridge/saybridge/internal/domain"
	"gorm.io/gorm"
)

// RoomSummary stores the cumulative sliding summary of a room's conversation history.
type RoomSummary struct {
	RoomID           string    `gorm:"primaryKey;type:uuid" json:"room_id"`
	Summary          string    `gorm:"type:text;not null" json:"summary"`
	LastMessageID    string    `gorm:"type:varchar(50)" json:"last_message_id"`
	LastSummarizedAt time.Time `gorm:"autoCreateTime" json:"last_summarized_at"`
	UpdatedAt        time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName overrides the GORM table name.
func (RoomSummary) TableName() string {
	return "room_summaries"
}

// AgentMemory stores semantic facts extracted from conversation history with pgvector embeddings.
type AgentMemory struct {
	ID         string    `gorm:"primaryKey;type:uuid" json:"id"`
	RoomID     string    `gorm:"type:uuid;index;not null" json:"room_id"`
	Content    string    `gorm:"type:text;not null" json:"content"`
	Embedding  string    `gorm:"type:text" json:"-"` // pgvector format, e.g. [0.1, 0.2, ...]
	Importance float64   `gorm:"type:double precision;default:1.0" json:"importance"`
	CreatedAt  time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName overrides the GORM table name.
func (AgentMemory) TableName() string {
	return "agent_memories"
}

// MemoryManager manages the room summaries and semantic memories.
type MemoryManager struct {
	db          *gorm.DB
	gateway     *Gateway
	messageRepo domain.MessageRepository
}

// NewMemoryManager creates a new MemoryManager instance.
func NewMemoryManager(db *gorm.DB, gw *Gateway, msgRepo domain.MessageRepository) *MemoryManager {
	return &MemoryManager{
		db:          db,
		gateway:     gw,
		messageRepo: msgRepo,
	}
}

// EnsureSchema creates the room_summaries and agent_memories tables.
func (mm *MemoryManager) EnsureSchema() error {
	if mm.db == nil {
		return fmt.Errorf("database not available")
	}

	// Enable pgvector extension
	if err := mm.db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error; err != nil {
		log.Printf("[MemoryManager] Warning: could not create vector extension: %v", err)
	}

	// AutoMigrate RoomSummary
	if err := mm.db.AutoMigrate(&RoomSummary{}); err != nil {
		return fmt.Errorf("automigrate room_summaries: %w", err)
	}

	// Create agent_memories with raw SQL for vector column
	createSQL := `
		CREATE TABLE IF NOT EXISTS agent_memories (
			id UUID PRIMARY KEY,
			room_id UUID NOT NULL,
			content TEXT NOT NULL,
			embedding vector(1536) NOT NULL,
			importance DOUBLE PRECISION DEFAULT 1.0,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`
	if err := mm.db.Exec(createSQL).Error; err != nil {
		return fmt.Errorf("create agent_memories table: %w", err)
	}

	// Indexes
	mm.db.Exec("CREATE INDEX IF NOT EXISTS idx_agent_memories_room ON agent_memories(room_id)")
	mm.db.Exec("CREATE INDEX IF NOT EXISTS idx_agent_memories_vector ON agent_memories USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)")

	log.Printf("[MemoryManager] ✓ Schema ensured (room_summaries and pgvector agent_memories)")
	return nil
}

// AgentMemorySearchResult represents a single memory search result with relevance score.
type AgentMemorySearchResult struct {
	ID         string    `json:"id"`
	RoomID     string    `json:"room_id"`
	Content    string    `json:"content"`
	Importance float64   `json:"importance"`
	CreatedAt  time.Time `json:"created_at"`
	Score      float64   `json:"score"`
}

// RetrieveRelevantMemories retrieves the top memories matching the user query with cosine similarity >= 0.65.
func (mm *MemoryManager) RetrieveRelevantMemories(ctx context.Context, roomID string, query string, limit int) ([]AgentMemorySearchResult, error) {
	if query == "" || roomID == "" || mm.db == nil {
		return nil, nil
	}

	// Generate query embedding via AI Gateway
	embeddings, err := mm.gateway.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, fmt.Errorf("empty embedding result")
	}

	queryVec := vectorToString(embeddings[0])

	var results []AgentMemorySearchResult
	sql := `SELECT id, room_id, content, importance, created_at, 1 - (embedding <=> ?::vector) AS score
			FROM agent_memories
			WHERE room_id = ? AND 1 - (embedding <=> ?::vector) >= 0.65
			ORDER BY embedding <=> ?::vector
			LIMIT ?`
	
	if err := mm.db.WithContext(ctx).Raw(sql, queryVec, roomID, queryVec, queryVec, limit).Scan(&results).Error; err != nil {
		log.Printf("[MemoryManager] Warning: semantic query failed (might not have pgvector): %v", err)
		return nil, nil // Return empty results on pgvector failure to ensure copilot keeps working
	}

	return results, nil
}

// CheckAndConsolidate checks and updates room summary and extracts new facts.
func (mm *MemoryManager) CheckAndConsolidate(roomID string) {
	if mm.db == nil || mm.messageRepo == nil {
		return
	}

	ctx := context.Background()

	// Get latest RoomSummary
	var summary RoomSummary
	err := mm.db.Where("room_id = ?", roomID).First(&summary).Error
	isNewSummary := err == gorm.ErrRecordNotFound

	lastMsgID := ""
	if !isNewSummary {
		lastMsgID = summary.LastMessageID
	}

	// Fetch message history
	messages, err := mm.messageRepo.GetMessageHistory(ctx, roomID, 50, "")
	if err != nil || len(messages) == 0 {
		return
	}

	// Filter out system messages, bot messages, deleted messages, or empty content
	var newMessages []domain.Message
	foundLastMsg := false

	// Loop from oldest to newest in retrieved window
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if lastMsgID != "" && m.MessageID == lastMsgID {
			foundLastMsg = true
			continue // skip this message and any older messages
		}
		if lastMsgID != "" && !foundLastMsg {
			// This message is older than lastMsgID, skip it
			continue
		}
		if m.IsDeleted || m.Content == "" || m.Content == "⏳" {
			continue
		}
		// Skip bot messages
		if strings.HasPrefix(m.SenderID, "00000000-0000-0000-0000-") || m.SenderID == domain.SystemActorID {
			continue
		}
		newMessages = append(newMessages, m)
	}

	// If we have less than 10 new messages, skip consolidation
	if len(newMessages) < 10 {
		return
	}

	// Format new messages for LLM context
	var sb strings.Builder
	for _, m := range newMessages {
		sb.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Content))
	}
	newMessagesText := sb.String()

	// 1. Update Summary
	summaryPrompt := `You are an AI conversation summarizer. Update the existing summary of the conversation with the new messages below.
Keep the summary under 100 words, highly concise, using a few bullet points of key states, tasks, and decisions. Do not repeat greeting small talk.

Existing Summary:
%s

New Messages:
%s

Output only the new updated summary.`
	
	existingSummaryText := "No previous summary."
	if !isNewSummary && summary.Summary != "" {
		existingSummaryText = summary.Summary
	}

	summaryReq := &ChatRequest{
		SystemPrompt: fmt.Sprintf(summaryPrompt, existingSummaryText, newMessagesText),
		Messages:     []ChatMessage{{Role: "user", Content: "Please update the summary based on the new messages."}},
		Temperature:  0.3,
		MaxTokens:    150,
	}

	summaryResp, err := mm.gateway.Query(ctx, summaryReq)
	if err != nil {
		log.Printf("[MemoryManager] Summarization failed: %v", err)
		return
	}
	updatedSummary := strings.TrimSpace(summaryResp.Content)

	// 2. Extract Key Facts
	factPrompt := `Analyze the conversation segment below and extract new permanent facts, user preferences, or decisions that are worth remembering for future interactions (e.g. user likes/dislikes, tech stack choice, deadlines, project details).
Ignore casual greetings, small talk, or temporary queries.
Return the result strictly as a JSON array of strings, e.g. ["User prefers Go over Python", "Project deadline is next Friday"].
If nothing important was discussed, return an empty JSON array: [].
Only return the JSON array, no other text or explanation.

New Messages:
%s`

	factReq := &ChatRequest{
		SystemPrompt: fmt.Sprintf(factPrompt, newMessagesText),
		Messages:     []ChatMessage{{Role: "user", Content: "Extract key facts as JSON array."}},
		Temperature:  0.0,
		MaxTokens:    200,
	}

	factResp, err := mm.gateway.Query(ctx, factReq)
	if err != nil {
		log.Printf("[MemoryManager] Fact extraction failed: %v", err)
		return
	}

	// Parse JSON array of facts
	var facts []string
	cleanContent := strings.TrimSpace(factResp.Content)
	if strings.Contains(cleanContent, "[") {
		startIdx := strings.Index(cleanContent, "[")
		endIdx := strings.LastIndex(cleanContent, "]")
		if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
			cleanContent = cleanContent[startIdx : endIdx+1]
		}
	}
	if err := json.Unmarshal([]byte(cleanContent), &facts); err != nil {
		log.Printf("[MemoryManager] Failed to parse facts JSON %q: %v", cleanContent, err)
	}

	// Save new facts
	if len(facts) > 0 {
		for _, fact := range facts {
			fact = strings.TrimSpace(fact)
			if fact == "" {
				continue
			}
			mm.deduplicateAndSaveFact(ctx, roomID, fact)
		}
	}

	// Update or create RoomSummary
	summary.RoomID = roomID
	summary.Summary = updatedSummary
	summary.LastMessageID = newMessages[len(newMessages)-1].MessageID
	summary.LastSummarizedAt = time.Now()
	summary.UpdatedAt = time.Now()

	if err := mm.db.Save(&summary).Error; err != nil {
		log.Printf("[MemoryManager] Failed to save room summary: %v", err)
	} else {
		log.Printf("[MemoryManager] Updated room summary for room %s (up to message %s)", roomID, summary.LastMessageID)
	}
}

// deduplicateAndSaveFact saves the fact if it's not similar to any existing fact in the room.
func (mm *MemoryManager) deduplicateAndSaveFact(ctx context.Context, roomID string, fact string) {
	if mm.db == nil {
		return
	}

	embeddings, err := mm.gateway.Embed(ctx, []string{fact})
	if err != nil || len(embeddings) == 0 || len(embeddings[0]) == 0 {
		log.Printf("[MemoryManager] Failed to generate embedding for fact: %v", err)
		return
	}

	factVec := vectorToString(embeddings[0])

	// Try to find if there is an existing fact with cosine similarity > 0.85
	var existing struct {
		ID    string
		Score float64
	}
	sql := `SELECT id, 1 - (embedding <=> ?::vector) AS score
			FROM agent_memories
			WHERE room_id = ? AND 1 - (embedding <=> ?::vector) > 0.85
			ORDER BY embedding <=> ?::vector
			LIMIT 1`
	
	err = mm.db.WithContext(ctx).Raw(sql, factVec, roomID, factVec).Scan(&existing).Error
	if err == nil && existing.ID != "" {
		// Fact already exists (highly similar), just update the updatedAt time
		mm.db.Exec("UPDATE agent_memories SET updated_at = NOW() WHERE id = ?", existing.ID)
		log.Printf("[MemoryManager] Fact already exists (similarity = %.2f), updated timestamp: %q", existing.Score, fact)
		return
	}

	// Insert new memory fact
	newID := uuid.New().String()
	insertSQL := `INSERT INTO agent_memories (id, room_id, content, embedding, importance, created_at, updated_at)
				  VALUES (?, ?, ?, ?::vector, 1.0, NOW(), NOW())`
	if err := mm.db.Exec(insertSQL, newID, roomID, fact, factVec).Error; err != nil {
		log.Printf("[MemoryManager] Failed to insert agent memory: %v", err)
	} else {
		log.Printf("[MemoryManager] Saved new agent memory: %q", fact)
	}
}
