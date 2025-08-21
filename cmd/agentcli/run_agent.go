package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/hyperifyio/goagent/internal/oai"
	"github.com/hyperifyio/goagent/internal/tools"
)

// runAgent executes the main chat completion flow with optional streaming and tools.
// nolint:gocyclo
func runAgent(cfg cliConfig, stdout io.Writer, stderr io.Writer) int {
	// Normalize timeouts for cases where cfg is constructed directly in tests
	if cfg.httpTimeout <= 0 {
		if cfg.timeout > 0 {
			cfg.httpTimeout = cfg.timeout
		} else {
			cfg.httpTimeout = 10 * time.Second
		}
	}
	if cfg.toolTimeout <= 0 {
		if cfg.timeout > 0 {
			cfg.toolTimeout = cfg.timeout
		} else {
			cfg.toolTimeout = 10 * time.Second
		}
	}

	// Load tools manifest if provided
	var (
		toolRegistry map[string]tools.ToolSpec
		oaiTools     []oai.Tool
	)
	if strings.TrimSpace(cfg.toolsPath) != "" {
		reg, toolsList, err := tools.LoadManifest(cfg.toolsPath)
		if err != nil {
			safeFprintf(stderr, "error: failed to load tools manifest: %v\n", err)
			return 1
		}
		// Verify each tool's program path is available
		for name, spec := range reg {
			if len(spec.Command) == 0 {
				safeFprintf(stderr, "error: configured tool %q has no command\n", name)
				return 1
			}
			if _, lookErr := exec.LookPath(spec.Command[0]); lookErr != nil {
				safeFprintf(stderr, "error: configured tool %q is unavailable: %v (program %q)\n", name, lookErr, spec.Command[0])
				return 1
			}
		}
		toolRegistry = reg
		oaiTools = toolsList
	}

	// Seed base transcript
	messages := seedMessages(cfg)

	// Optional pre-stage before main call (enabled by default unless explicitly disabled)
	if cfg.prepEnabled {
		if out, err := runPreStage(cfg, messages, stderr); err == nil {
			messages = out
		} else {
			// Fail-open: log concise warning and continue
			safeFprintf(stderr, "WARN: pre-stage failed; skipping (reason: %s)\n", oneLine(err.Error()))
		}
	}

	// Create client with retries
	client := oai.NewClientWithRetry(cfg.baseURL, cfg.apiKey, cfg.httpTimeout, oai.RetryPolicy{MaxRetries: cfg.httpRetries, Backoff: cfg.httpBackoff})

	// Streaming path
	if cfg.streamFinal {
		req := oai.ChatCompletionsRequest{Model: cfg.model, Messages: applyTranscriptHygiene(messages, cfg.debug)}
		if cfg.topP > 0 {
			t := cfg.topP
			req.TopP = &t
		} else if oai.SupportsTemperature(cfg.model) {
			t := cfg.temperature
			req.Temperature = &t
		}
		return runAgentStream(context.WithValue(context.Background(), auditStageKey{}, "main"), client, req, stdout, stderr)
	}

	// Multi-step loop with tool execution
	maxSteps := cfg.maxSteps
	if maxSteps <= 0 {
		maxSteps = 4
	}
	if maxSteps > 15 {
		maxSteps = 15
	}
	for step := 0; step < maxSteps; step++ {
		req := oai.ChatCompletionsRequest{
			Model:    cfg.model,
			Messages: applyTranscriptHygiene(messages, cfg.debug),
		}
		// One‑knob: top_p wins, else temperature if supported
		if cfg.topP > 0 {
			t := cfg.topP
			req.TopP = &t
		} else if oai.SupportsTemperature(cfg.model) {
			t := cfg.temperature
			req.Temperature = &t
		}
		if len(oaiTools) > 0 {
			req.Tools = oaiTools
			req.ToolChoice = "auto"
		}

		ctx, cancel := context.WithTimeout(oai.WithAuditStage(context.Background(), "main"), cfg.httpTimeout)
		resp, err := client.CreateChatCompletion(ctx, req)
		cancel()
		if err != nil {
			safeFprintf(stderr, "error: request failed: %v\n", err)
			return 1
		}
		if len(resp.Choices) == 0 {
			continue
		}
		msg := resp.Choices[0].Message
		// If tool calls requested and we have a registry, execute them then continue
		if len(msg.ToolCalls) > 0 && len(toolRegistry) > 0 {
			messages = append(messages, msg)
			messages = appendToolCallOutputs(messages, msg, toolRegistry, cfg)
			continue
		}
		// Channel-aware printing: print only final channel to stdout by default
		if msg.Role == oai.RoleAssistant && strings.TrimSpace(msg.Content) != "" {
			ch := strings.TrimSpace(msg.Channel)
			if ch == "final" || ch == "" {
				safeFprintln(stdout, strings.TrimSpace(msg.Content))
				// Debug dump after human-readable output
				dumpJSONIfDebug(stderr, fmt.Sprintf("chat.response step=%d", step+1), resp, cfg.debug)
				return 0
			}
			// Non-final assistant content: under -verbose, route per config (default omit)
			if cfg.verbose {
				dest := resolveChannelRoute(cfg, ch, true)
				switch dest {
				case "stdout":
					safeFprintln(stdout, strings.TrimSpace(msg.Content))
				case "stderr":
					safeFprintln(stderr, strings.TrimSpace(msg.Content))
				}
			}
			messages = append(messages, msg)
			continue
		}
		// Append any assistant message and continue loop
		messages = append(messages, msg)
	}
	// Reached max steps without final output
	safeFprintln(stderr, fmt.Sprintf("info: reached maximum steps (%d); needs review", maxSteps))
	return 1
}

// runAgentStream handles SSE streaming and prints only assistant{channel:"final"} to stdout.
func runAgentStream(ctx context.Context, client *oai.Client, req oai.ChatCompletionsRequest, stdout io.Writer, stderr io.Writer) int {
	err := client.StreamChat(ctx, req, func(chunk oai.StreamChunk) error {
		for _, ch := range chunk.Choices {
			c := strings.TrimSpace(ch.Delta.Content)
			if c != "" && strings.TrimSpace(ch.Delta.Channel) == "final" {
				_, _ = io.WriteString(stdout, c)
			}
		}
		return nil
	})
	if errors.Is(err, context.DeadlineExceeded) {
		safeFprintln(stderr, "error: stream timed out")
		return 1
	}
	if err != nil {
		safeFprintf(stderr, "error: stream request failed: %v\n", err)
		return 1
	}
	// Finish with newline for TTY friendliness
	_, _ = io.WriteString(stdout, "\n")
	return 0
}

// seedMessages constructs the initial [system,user] transcript.
func seedMessages(cfg cliConfig) []oai.Message {
	msgs := make([]oai.Message, 0, 2)
	if s := strings.TrimSpace(cfg.systemPrompt); s != "" {
		msgs = append(msgs, oai.Message{Role: oai.RoleSystem, Content: s})
	}
	msgs = append(msgs, oai.Message{Role: oai.RoleUser, Content: strings.TrimSpace(cfg.prompt)})
	return msgs
}

// safe HTTP client used by tests when intercepting; retained for parity
var _ http.RoundTripper

type auditStageKey struct{}
