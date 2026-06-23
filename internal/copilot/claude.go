package copilot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ClaudeProvider struct {
	apiKey  string
	baseURL string
	model   string
}

func NewClaudeProvider(apiKey, baseURL, model string) *ClaudeProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	if model == "" {
		model = "claude-opus-4-8"
	}
	return &ClaudeProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
	}
}

func (p *ClaudeProvider) ID() string   { return "claude" }
func (p *ClaudeProvider) Name() string { return "Claude" }

// ── Wire types ────────────────────────────────────────────────────────────────

// claudeContentBlock is a single block in a message's content array. Different
// block types populate different fields; omitempty keeps each block minimal.
type claudeContentBlock struct {
	Type string `json:"type"` // "text" | "tool_use" | "tool_result"

	// text
	Text string `json:"text,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type claudeReqMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"` // string OR []claudeContentBlock
}

type claudeTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type claudeChatRequest struct {
	Model       string             `json:"model"`
	Messages    []claudeReqMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Tools       []claudeTool       `json:"tools,omitempty"`
	ToolChoice  interface{}        `json:"tool_choice,omitempty"`
}

type claudeChatResponse struct {
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type claudeStreamChunk struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

// ── Request building ──────────────────────────────────────────────────────────

// buildMessages converts provider-agnostic ChatMessages into Claude's wire
// format. Plain text turns use the simple string content form; turns carrying
// tool calls or tool results use the content-block array form.
func buildClaudeMessages(msgs []ChatMessage) []claudeReqMessage {
	out := make([]claudeReqMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "system" {
			continue // system prompt is sent in the top-level "system" field
		}

		switch {
		case len(m.ToolResults) > 0:
			blocks := make([]claudeContentBlock, 0, len(m.ToolResults))
			for _, r := range m.ToolResults {
				blocks = append(blocks, claudeContentBlock{
					Type:      "tool_result",
					ToolUseID: r.ToolUseID,
					Content:   r.Content,
					IsError:   r.IsError,
				})
			}
			out = append(out, claudeReqMessage{Role: "user", Content: blocks})

		case len(m.ToolCalls) > 0:
			blocks := make([]claudeContentBlock, 0, len(m.ToolCalls)+1)
			if m.Content != "" {
				blocks = append(blocks, claudeContentBlock{Type: "text", Text: m.Content})
			}
			for _, c := range m.ToolCalls {
				input := c.Input
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, claudeContentBlock{
					Type:  "tool_use",
					ID:    c.ID,
					Name:  c.Name,
					Input: input,
				})
			}
			out = append(out, claudeReqMessage{Role: "assistant", Content: blocks})

		default:
			out = append(out, claudeReqMessage{Role: m.Role, Content: m.Content})
		}
	}
	return out
}

func toClaudeTools(tools []ToolDefinition) []claudeTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]claudeTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, claudeTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return out
}

func toClaudeToolChoice(choice string) interface{} {
	switch choice {
	case "", "auto":
		return nil
	case "any", "none":
		return map[string]string{"type": choice}
	default:
		return map[string]string{"type": "tool", "name": choice}
	}
}

func (p *ClaudeProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	bodyObj := claudeChatRequest{
		Model:       model,
		Messages:    buildClaudeMessages(req.Messages),
		System:      req.SystemPrompt,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		Tools:       toClaudeTools(req.Tools),
		ToolChoice:  toClaudeToolChoice(req.ToolChoice),
	}

	bodyBytes, err := json.Marshal(bodyObj)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("claude error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var chatResp claudeChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var contentText strings.Builder
	var toolCalls []ToolCall
	for _, c := range chatResp.Content {
		switch c.Type {
		case "text":
			contentText.WriteString(c.Text)
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{ID: c.ID, Name: c.Name, Input: c.Input})
		}
	}

	stopReason := chatResp.StopReason
	if stopReason == "" {
		stopReason = "end_turn"
	}

	return &ChatResponse{
		Content:      contentText.String(),
		Model:        chatResp.Model,
		InputTokens:  chatResp.Usage.InputTokens,
		OutputTokens: chatResp.Usage.OutputTokens,
		FinishReason: stopReason,
		StopReason:   stopReason,
		ToolCalls:    toolCalls,
	}, nil
}

func (p *ClaudeProvider) ChatStream(ctx context.Context, req *ChatRequest, ch chan<- StreamChunk) error {
	defer close(ch)

	model := req.Model
	if model == "" {
		model = p.model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	bodyObj := claudeChatRequest{
		Model:       model,
		Messages:    buildClaudeMessages(req.Messages),
		System:      req.SystemPrompt,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		Stream:      true,
	}

	bodyBytes, err := json.Marshal(bodyObj)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("execute http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("claude error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("read stream line: %w", err)
		}

		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		dataStr := strings.TrimPrefix(line, "data: ")

		var chunk claudeStreamChunk
		if err := json.Unmarshal([]byte(dataStr), &chunk); err != nil {
			continue
		}

		if chunk.Type == "content_block_delta" && chunk.Delta.Type == "text_delta" {
			if chunk.Delta.Text != "" {
				ch <- StreamChunk{Content: chunk.Delta.Text}
			}
		}
		if chunk.Type == "message_stop" {
			ch <- StreamChunk{Done: true}
			break
		}
	}

	return nil
}

func (p *ClaudeProvider) Embeddings(ctx context.Context, texts []string) ([][]float32, error) {
	// Claude doesn't have a standard embeddings API, return mock or error
	return nil, fmt.Errorf("embeddings are not supported by Claude provider")
}

// SupportsEmbeddings reports false: Anthropic has no embeddings endpoint.
func (p *ClaudeProvider) SupportsEmbeddings() bool { return false }

func (p *ClaudeProvider) SetAPIKey(apiKey string) {
	p.apiKey = apiKey
}
