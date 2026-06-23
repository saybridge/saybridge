package copilot

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
)

// ── AI Tool framework ─────────────────────────────────────────────────────────
//
// This is the heart of an "AI-native" platform: the AI agent doesn't just talk,
// it *acts*. Core features and plugins register tools here; the agent loop
// exposes them to the model and dispatches the model's tool calls back to their
// handlers. Every capability registered becomes something an agent can do.

// ToolDefinition is the provider-agnostic description of a callable tool. The
// shape mirrors the JSON Schema that every major LLM tool-use API expects
// (Anthropic `input_schema`, OpenAI `parameters`, etc.).
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ToolCall is a single tool invocation requested by the model.
type ToolCall struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ToolResult is the outcome of executing a ToolCall, fed back to the model.
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}

// ToolHandler executes a tool call and returns a string result. Returning an
// error surfaces to the model as an error tool result so it can recover.
type ToolHandler func(ctx context.Context, input json.RawMessage) (string, error)

// Tool couples a definition with its executor.
type Tool struct {
	Definition ToolDefinition
	Handler    ToolHandler
}

// ToolRegistry holds the set of tools available to AI agents.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// DefaultToolRegistry is the global registry that core features and plugins
// register into.
var DefaultToolRegistry = NewToolRegistry()

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

// Register adds (or replaces) a tool by name.
func (r *ToolRegistry) Register(t Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Definition.Name] = t
}

// Definitions returns all registered tool definitions, sorted by name for a
// stable order (which keeps prompt caching effective).
func (r *ToolRegistry) Definitions() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs
}

// Len returns the number of registered tools.
func (r *ToolRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// Execute runs a tool by name. Unknown tools and handler errors are returned as
// error ToolResults (never panics) so the agent loop can feed them back to the
// model rather than aborting.
func (r *ToolRegistry) Execute(ctx context.Context, call ToolCall) ToolResult {
	r.mu.RLock()
	t, ok := r.tools[call.Name]
	r.mu.RUnlock()
	if !ok {
		return ToolResult{ToolUseID: call.ID, Content: "unknown tool: " + call.Name, IsError: true}
	}

	out, err := func() (out string, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				err = errFromPanic(rec)
			}
		}()
		return t.Handler(ctx, call.Input)
	}()
	if err != nil {
		return ToolResult{ToolUseID: call.ID, Content: "tool error: " + err.Error(), IsError: true}
	}
	return ToolResult{ToolUseID: call.ID, Content: out}
}
