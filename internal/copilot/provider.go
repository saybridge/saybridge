package copilot

import "context"

// Provider interface — implemented by all AI providers
type Provider interface {
	ID() string                    // "openai", "claude", "gemini", "ollama"
	Name() string                  // Human-readable name
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req *ChatRequest, ch chan<- StreamChunk) error
	Embeddings(ctx context.Context, texts []string) ([][]float32, error)
	// SupportsEmbeddings reports whether this provider can produce embeddings.
	// The gateway uses this to route embedding requests away from chat-only
	// providers (e.g. Claude) to one that supports them.
	SupportsEmbeddings() bool
}

type ChatRequest struct {
	Model        string
	Messages     []ChatMessage
	MaxTokens    int
	Temperature  float64
	SystemPrompt string

	// Tools, when non-empty, enables tool use (function calling). Providers that
	// support it advertise these to the model; the model may respond with
	// ToolCalls instead of (or alongside) text.
	Tools []ToolDefinition
	// ToolChoice optionally forces behavior: "" (auto), "any", "none", or a
	// specific tool name.
	ToolChoice string
}

type ChatMessage struct {
	Role    string // "user", "assistant", "system"
	Content string

	// ToolCalls holds tool invocations on an assistant turn (echoed back so the
	// model has the full context when continuing a tool-use conversation).
	ToolCalls []ToolCall
	// ToolResults holds tool outputs on a user turn, answering prior ToolCalls.
	ToolResults []ToolResult
}

type ChatResponse struct {
	Content      string `json:"content"`
	Model        string `json:"model"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	FinishReason string `json:"finish_reason"`

	// StopReason is the provider's raw stop reason ("end_turn", "tool_use", ...).
	StopReason string `json:"stop_reason"`
	// ToolCalls holds any tool invocations the model requested this turn.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type StreamChunk struct {
	Content string
	Done    bool
	Error   error
}
