package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/saybridge/saybridge/internal/domain"
	"github.com/saybridge/saybridge/pkg/response"
)

// registerBuiltinTools wires core, read-only capabilities into the tool registry
// so AI agents can act on the workspace. Kept read-only for safety; write tools
// (post message, create room, run workflow) can be added behind permissions.
func registerBuiltinTools(reg *ToolRegistry, semIdx *SemanticIndex, messageRepo domain.MessageRepository) {
	if reg == nil {
		return
	}

	if semIdx != nil {
		reg.Register(Tool{
			Definition: ToolDefinition{
				Name:        "search_workspace",
				Description: "Semantically search the workspace's message history for relevant past context. Call this when answering a question that may depend on earlier conversations.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "What to search for, in natural language.",
						},
					},
					"required": []string{"query"},
				},
			},
			Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
				var args struct {
					Query string `json:"query"`
				}
				if err := json.Unmarshal(input, &args); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if strings.TrimSpace(args.Query) == "" {
					return "", fmt.Errorf("query is required")
				}
				results, err := semIdx.Search(ctx, args.Query, nil, 5)
				if err != nil {
					return "", err
				}
				if len(results) == 0 {
					return "No relevant messages found.", nil
				}
				var sb strings.Builder
				for i, r := range results {
					sb.WriteString(fmt.Sprintf("%d. (score %.2f) %s\n", i+1, r.Score, r.Content))
				}
				return sb.String(), nil
			},
		})
	}

	if messageRepo != nil {
		reg.Register(Tool{
			Definition: ToolDefinition{
				Name:        "get_room_history",
				Description: "Fetch the most recent messages from a specific room. Use this to understand the current conversation in a room before answering.",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"room_id": map[string]interface{}{
							"type":        "string",
							"description": "The room ID to read.",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "How many recent messages to fetch (default 20, max 50).",
						},
					},
					"required": []string{"room_id"},
				},
			},
			Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
				var args struct {
					RoomID string `json:"room_id"`
					Limit  int    `json:"limit"`
				}
				if err := json.Unmarshal(input, &args); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
				if args.RoomID == "" {
					return "", fmt.Errorf("room_id is required")
				}
				if args.Limit <= 0 {
					args.Limit = 20
				}
				if args.Limit > 50 {
					args.Limit = 50
				}
				history, err := messageRepo.GetMessageHistory(ctx, args.RoomID, args.Limit, "")
				if err != nil {
					return "", err
				}
				if len(history) == 0 {
					return "No messages in this room.", nil
				}
				var sb strings.Builder
				for i := len(history) - 1; i >= 0; i-- {
					m := history[i]
					if m.IsDeleted || m.Content == "" {
						continue
					}
					sb.WriteString(fmt.Sprintf("%s: %s\n", m.SenderName, m.Content))
				}
				return sb.String(), nil
			},
		})
	}
}

// agent handles POST /api/v1/copilot/agent — a one-shot agentic request that
// can call tools (search the workspace, read room history) to answer.
func (h *aiHandler) agent(c *gin.Context) {
	var req struct {
		Message string `json:"message" binding:"required"`
		RoomID  string `json:"room_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	systemPrompt := "You are a helpful AI assistant inside a team workspace. Use the available tools to gather context before answering when it would help."
	if DefaultAgentConfig != nil && DefaultAgentConfig.SystemPrompt != "" {
		systemPrompt = DefaultAgentConfig.SystemPrompt
	}
	systemPrompt += PromptProtectionInstructions

	userMsg := req.Message
	if req.RoomID != "" {
		userMsg = fmt.Sprintf("[current room_id: %s]\n%s", req.RoomID, req.Message)
	}

	maxTokens := 4096
	if DefaultAgentConfig != nil && DefaultAgentConfig.MaxTokens > 0 {
		maxTokens = DefaultAgentConfig.MaxTokens
	}

	chatReq := &ChatRequest{
		SystemPrompt: systemPrompt,
		Messages:     []ChatMessage{{Role: "user", Content: userMsg}},
		MaxTokens:    maxTokens,
	}

	result, err := RunAgentLoop(c.Request.Context(), h.gateway, DefaultToolRegistry, chatReq, 6)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "AGENT_FAILED", err.Error())
		return
	}

	response.JSON(c, http.StatusOK, gin.H{
		"content":    result.Content,
		"tools_used": result.ToolsUsed,
		"iterations": result.Iterations,
	})
}
