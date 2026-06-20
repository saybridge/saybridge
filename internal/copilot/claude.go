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
		model = "claude-3-5-sonnet-20240620"
	}
	return &ClaudeProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
	}
}

func (p *ClaudeProvider) ID() string   { return "claude" }
func (p *ClaudeProvider) Name() string { return "Claude" }

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeChatRequest struct {
	Model       string          `json:"model"`
	Messages    []claudeMessage `json:"messages"`
	System      string          `json:"system,omitempty"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature float64         `json:"temperature,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type claudeChatResponse struct {
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type claudeStreamChunk struct {
	Type  string `json:"type"` // "content_block_delta", etc.
	Delta struct {
		Type string `json:"type"` // "text_delta"
		Text string `json:"text"`
	} `json:"delta"`
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

	messages := make([]claudeMessage, 0, len(req.Messages))
	for _, msg := range req.Messages {
		role := msg.Role
		if role == "system" {
			// System prompt is handled separately in Claude API
			continue
		}
		messages = append(messages, claudeMessage{Role: role, Content: msg.Content})
	}

	bodyObj := claudeChatRequest{
		Model:       model,
		Messages:    messages,
		System:      req.SystemPrompt,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
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

	var contentText string
	for _, c := range chatResp.Content {
		if c.Type == "text" {
			contentText = c.Text
			break
		}
	}

	return &ChatResponse{
		Content:      contentText,
		Model:        chatResp.Model,
		InputTokens:  chatResp.Usage.InputTokens,
		OutputTokens: chatResp.Usage.OutputTokens,
		FinishReason: "end_turn",
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

	messages := make([]claudeMessage, 0, len(req.Messages))
	for _, msg := range req.Messages {
		role := msg.Role
		if role == "system" {
			continue
		}
		messages = append(messages, claudeMessage{Role: role, Content: msg.Content})
	}

	bodyObj := claudeChatRequest{
		Model:       model,
		Messages:    messages,
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

func (p *ClaudeProvider) SetAPIKey(apiKey string) {
	p.apiKey = apiKey
}
