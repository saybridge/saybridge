package copilot

import (
	"context"
	"strings"
	"testing"

	"github.com/saybridge/saybridge/internal/domain"
)

type MockProvider struct {
	chatFn  func(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	embedFn func(ctx context.Context, texts []string) ([][]float32, error)
}

func (m *MockProvider) ID() string                { return "gemini" }
func (m *MockProvider) Name() string              { return "Mock Provider" }
func (m *MockProvider) SupportsEmbeddings() bool  { return true }
func (m *MockProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if m.chatFn != nil {
		return m.chatFn(ctx, req)
	}
	return &ChatResponse{Content: "[]"}, nil
}
func (m *MockProvider) ChatStream(ctx context.Context, req *ChatRequest, ch chan<- StreamChunk) error {
	close(ch)
	return nil
}
func (m *MockProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if m.embedFn != nil {
		return m.embedFn(ctx, texts)
	}
	res := make([][]float32, len(texts))
	for i := range texts {
		res[i] = make([]float32, 1536)
	}
	return res, nil
}

type MockMessageRepository struct {
	messages []domain.Message
}

func (m *MockMessageRepository) SaveMessage(ctx context.Context, msg *domain.Message) error { return nil }
func (m *MockMessageRepository) GetMessageHistory(ctx context.Context, roomID string, limit int, beforeID string) ([]domain.Message, error) {
	return m.messages, nil
}
func (m *MockMessageRepository) UpdateMessage(ctx context.Context, msg *domain.Message) error {
	return nil
}
func (m *MockMessageRepository) UpdateMessageContent(ctx context.Context, messageID, content string) error {
	return nil
}
func (m *MockMessageRepository) GetMessage(ctx context.Context, roomID string, timeBucket int, messageID string) (*domain.Message, error) {
	return nil, nil
}
func (m *MockMessageRepository) SaveThreadReply(ctx context.Context, msg *domain.Message) error {
	return nil
}
func (m *MockMessageRepository) GetThreadReplies(ctx context.Context, parentID string) ([]domain.Message, error) {
	return nil, nil
}
func (m *MockMessageRepository) GetThreadCounters(ctx context.Context, parentIDs []string) (map[string]int, error) {
	return nil, nil
}

func TestMemoryManager_GracefulFallbackWithNilDB(t *testing.T) {
	gw := NewGateway("gemini")
	mockProv := &MockProvider{}
	gw.RegisterProvider(mockProv)

	mockRepo := &MockMessageRepository{
		messages: []domain.Message{
			{MessageID: "1", SenderName: "Paul", Content: "Hello assistant!"},
			{MessageID: "2", SenderName: "Sai", Content: "Hello Paul!"},
		},
	}

	// mm has nil db, should not panic and should fail gracefully
	mm := NewMemoryManager(nil, gw, mockRepo)

	ctx := context.Background()

	// EnsureSchema should return error instead of panicking
	err := mm.EnsureSchema()
	if err == nil {
		t.Error("Expected error from EnsureSchema with nil DB, got nil")
	}

	// RetrieveRelevantMemories should return nil, nil instead of panicking
	mems, err := mm.RetrieveRelevantMemories(ctx, "room-1", "Go project", 2)
	if err != nil {
		t.Errorf("RetrieveRelevantMemories returned unexpected error: %v", err)
	}
	if len(mems) != 0 {
		t.Errorf("Expected 0 memories, got %d", len(mems))
	}

	// CheckAndConsolidate with nil DB should return/fallback gracefully without panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("CheckAndConsolidate panicked: %v", r)
		}
	}()
	mm.CheckAndConsolidate("room-1")
}

func TestPromptProtectionInstructionsAppended(t *testing.T) {
	gw := NewGateway("gemini")
	mockProv := &MockProvider{}
	gw.RegisterProvider(mockProv)

	mockRepo := &MockMessageRepository{
		messages: []domain.Message{
			{MessageID: "1", SenderName: "Paul", Content: "Hello assistant!"},
			{MessageID: "2", SenderName: "Sai", Content: "Hello Paul!"},
		},
	}

	h := &aiHandler{
		gateway:       gw,
		messageRepo:   mockRepo,
		memoryManager: NewMemoryManager(nil, gw, mockRepo),
		db:            nil,
	}

	ctx := context.Background()
	defaultPrompt := "You are a coding assistant."
	augmentedPrompt, messages := h.buildAugmentedRequest(ctx, "room-1", "How do I reverse a string in Go?", defaultPrompt)

	if !strings.HasSuffix(augmentedPrompt, PromptProtectionInstructions) {
		t.Error("Expected augmentedPrompt to have PromptProtectionInstructions appended to the end")
	}

	if len(messages) != 3 {
		t.Errorf("Expected 3 messages (2 history + 1 current prompt), got %d", len(messages))
	}

	if messages[len(messages)-1].Content != "How do I reverse a string in Go?" {
		t.Errorf("Expected last message to be user prompt, got %q", messages[len(messages)-1].Content)
	}
}
