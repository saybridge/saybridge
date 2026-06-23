package copilot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestToolRegistry_ExecuteAndDefinitions(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(Tool{
		Definition: ToolDefinition{Name: "echo", Description: "echoes input"},
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			return "echo:" + string(input), nil
		},
	})
	reg.Register(Tool{Definition: ToolDefinition{Name: "alpha"}})

	if reg.Len() != 2 {
		t.Fatalf("Len = %d, want 2", reg.Len())
	}

	// Definitions are sorted by name.
	defs := reg.Definitions()
	if defs[0].Name != "alpha" || defs[1].Name != "echo" {
		t.Fatalf("definitions not sorted: %v", defs)
	}

	// Execute a known tool.
	res := reg.Execute(context.Background(), ToolCall{ID: "t1", Name: "echo", Input: json.RawMessage(`{"x":1}`)})
	if res.IsError || res.Content != `echo:{"x":1}` || res.ToolUseID != "t1" {
		t.Fatalf("unexpected result: %+v", res)
	}

	// Unknown tool → error result, never panics.
	res = reg.Execute(context.Background(), ToolCall{ID: "t2", Name: "nope"})
	if !res.IsError {
		t.Fatalf("expected error result for unknown tool, got %+v", res)
	}
}

func TestRunAgentLoop_ExecutesToolsThenAnswers(t *testing.T) {
	reg := NewToolRegistry()
	var toolRan bool
	reg.Register(Tool{
		Definition: ToolDefinition{Name: "lookup"},
		Handler: func(ctx context.Context, input json.RawMessage) (string, error) {
			toolRan = true
			return "42", nil
		},
	})

	// Mock provider: first turn requests a tool, second turn gives the answer.
	call := 0
	mock := &MockProvider{
		chatFn: func(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
			call++
			if call == 1 {
				// The loop must have advertised the registry's tools.
				if len(req.Tools) != 1 || req.Tools[0].Name != "lookup" {
					t.Fatalf("tools not advertised to provider: %+v", req.Tools)
				}
				return &ChatResponse{
					StopReason: "tool_use",
					ToolCalls:  []ToolCall{{ID: "tu1", Name: "lookup", Input: json.RawMessage(`{}`)}},
				}, nil
			}
			// On the second call the tool result must be in the history.
			last := req.Messages[len(req.Messages)-1]
			if len(last.ToolResults) != 1 || last.ToolResults[0].Content != "42" {
				t.Fatalf("tool result not fed back: %+v", req.Messages)
			}
			return &ChatResponse{StopReason: "end_turn", Content: "The answer is 42."}, nil
		},
	}

	gw := NewGateway("gemini")
	gw.RegisterProvider(mock)

	req := &ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "what is it?"}}}
	result, err := RunAgentLoop(context.Background(), gw, reg, req, 6)
	if err != nil {
		t.Fatalf("RunAgentLoop error: %v", err)
	}
	if !toolRan {
		t.Fatal("tool handler was not executed")
	}
	if !strings.Contains(result.Content, "42") {
		t.Fatalf("final content = %q, want it to contain 42", result.Content)
	}
	if result.Iterations != 2 {
		t.Fatalf("iterations = %d, want 2", result.Iterations)
	}
	if len(result.ToolsUsed) != 1 || result.ToolsUsed[0] != "lookup" {
		t.Fatalf("tools used = %v, want [lookup]", result.ToolsUsed)
	}
}

func TestRunAgentLoop_NoToolsJustAnswers(t *testing.T) {
	mock := &MockProvider{
		chatFn: func(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
			return &ChatResponse{StopReason: "end_turn", Content: "hi"}, nil
		},
	}
	gw := NewGateway("gemini")
	gw.RegisterProvider(mock)

	req := &ChatRequest{Messages: []ChatMessage{{Role: "user", Content: "hello"}}}
	result, err := RunAgentLoop(context.Background(), gw, NewToolRegistry(), req, 6)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.Content != "hi" || result.Iterations != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
