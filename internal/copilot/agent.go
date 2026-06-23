package copilot

import (
	"context"
	"fmt"
)

// AgentResult is the outcome of an agent loop run.
type AgentResult struct {
	Content    string
	ToolsUsed  []string
	Iterations int
}

// RunAgentLoop drives a multi-turn tool-use conversation. It advertises the
// registry's tools to the model and, whenever the model responds with tool
// calls, executes them via the registry, appends the results, and loops — until
// the model produces a final text answer or maxIters is reached.
//
// This is what makes the copilot "agentic": the model decides which tools to
// call and the loop carries out those actions, rather than only answering from
// the prompt.
func RunAgentLoop(ctx context.Context, gw *Gateway, registry *ToolRegistry, req *ChatRequest, maxIters int) (*AgentResult, error) {
	if gw == nil {
		return nil, fmt.Errorf("agent loop requires an AI gateway")
	}
	if maxIters <= 0 {
		maxIters = 6
	}
	if registry != nil && len(registry.Definitions()) > 0 {
		req.Tools = registry.Definitions()
	}

	result := &AgentResult{}

	for i := 0; i < maxIters; i++ {
		result.Iterations = i + 1

		resp, err := gw.Query(ctx, req)
		if err != nil {
			return nil, err
		}

		// No tool calls → the model produced its final answer.
		if registry == nil || resp.StopReason != "tool_use" || len(resp.ToolCalls) == 0 {
			result.Content = resp.Content
			return result, nil
		}

		// Echo the assistant turn (text + tool_use) so the model keeps context.
		req.Messages = append(req.Messages, ChatMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each requested tool and collect results into one user turn.
		results := make([]ToolResult, 0, len(resp.ToolCalls))
		for _, call := range resp.ToolCalls {
			result.ToolsUsed = append(result.ToolsUsed, call.Name)
			results = append(results, registry.Execute(ctx, call))
		}
		req.Messages = append(req.Messages, ChatMessage{Role: "user", ToolResults: results})
	}

	return result, fmt.Errorf("agent loop exceeded %d iterations without completing", maxIters)
}

// errFromPanic converts a recovered panic value into an error.
func errFromPanic(rec interface{}) error {
	if err, ok := rec.(error); ok {
		return err
	}
	return fmt.Errorf("%v", rec)
}
