package copilot

import "context"

// Provider interface — implemented by all AI providers
type Provider interface {
	ID() string                    // "openai", "claude", "gemini", "ollama"
	Name() string                  // Human-readable name
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req *ChatRequest, ch chan<- StreamChunk) error
	Embeddings(ctx context.Context, texts []string) ([][]float32, error)
}

type ChatRequest struct {
	Model        string
	Messages     []ChatMessage
	MaxTokens    int
	Temperature  float64
	SystemPrompt string
}

type ChatMessage struct {
	Role    string // "user", "assistant", "system"
	Content string
}

type ChatResponse struct {
	Content      string `json:"content"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	FinishReason string `json:"finish_reason"`
}

type StreamChunk struct {
	Content string
	Done    bool
	Error   error
}
