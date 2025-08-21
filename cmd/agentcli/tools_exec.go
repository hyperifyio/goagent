package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/hyperifyio/goagent/internal/oai"
	"github.com/hyperifyio/goagent/internal/tools"
)

type toolResult struct {
	msg oai.Message
}

// appendToolCallOutputs executes assistant-requested tool calls and appends their outputs.
func appendToolCallOutputs(messages []oai.Message, assistantMsg oai.Message, toolRegistry map[string]tools.ToolSpec, cfg cliConfig) []oai.Message {
	results := make(chan toolResult, len(assistantMsg.ToolCalls))

	// Launch each tool call concurrently
	for _, tc := range assistantMsg.ToolCalls {
		toolCall := tc // capture loop var
		spec, exists := toolRegistry[toolCall.Function.Name]
		if !exists {
			// Unknown tool: synthesize deterministic error JSON
			go func() {
				content := sanitizeToolContent(nil, fmt.Errorf("unknown tool: %s", toolCall.Function.Name))
				results <- toolResult{msg: oai.Message{Role: oai.RoleTool, Name: toolCall.Function.Name, ToolCallID: toolCall.ID, Content: content}}
			}()
			continue
		}

		go func(spec tools.ToolSpec, toolCall oai.ToolCall) {
			argsJSON := strings.TrimSpace(toolCall.Function.Arguments)
			if argsJSON == "" {
				argsJSON = "{}"
			}
			out, runErr := tools.RunToolWithJSON(context.Background(), spec, []byte(argsJSON), cfg.toolTimeout)
			content := sanitizeToolContent(out, runErr)
			results <- toolResult{msg: oai.Message{Role: oai.RoleTool, Name: toolCall.Function.Name, ToolCallID: toolCall.ID, Content: content}}
		}(spec, toolCall)
	}

	// Collect exactly one result per requested tool call
	for i := 0; i < len(assistantMsg.ToolCalls); i++ {
		r := <-results
		messages = append(messages, r.msg)
	}
	return messages
}
