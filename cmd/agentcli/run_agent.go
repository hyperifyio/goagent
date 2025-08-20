package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/hyperifyio/goagent/internal/oai"
	"github.com/hyperifyio/goagent/internal/tools"
)

// runAgent executes the non-interactive agent loop and returns a process exit code.
// nolint:gocyclo // Orchestrates the agent loop; complexity is acceptable and covered by tests.
func runAgent(cfg cliConfig, stdout io.Writer, stderr io.Writer) int {
	// Default pre-stage enabled when not explicitly set (covers tests constructing cfg directly)
	if !cfg.prepEnabledSet {
		cfg.prepEnabled = true
	}
	// Normalize timeouts for backward compatibility when cfg constructed directly in tests
	if cfg.httpTimeout <= 0 {
		if cfg.timeout > 0 {
			cfg.httpTimeout = cfg.timeout
		} else {
			cfg.httpTimeout = 90 * time.Second
		}
	}
	// Emit effective timeout sources under -debug (after normalization)
	if cfg.debug {
		safeFprintf(stderr, "effective timeouts: http-timeout=%s source=%s; prep-http-timeout=%s source=%s; tool-timeout=%s source=%s; timeout=%s source=%s\n",
			cfg.httpTimeout.String(), cfg.httpTimeoutSource,
			cfg.prepHTTPTimeout.String(), cfg.prepHTTPTimeoutSource,
			cfg.toolTimeout.String(), cfg.toolTimeoutSource,
			cfg.timeout.String(), cfg.globalTimeoutSource,
		)
	}
	if cfg.toolTimeout <= 0 {
		if cfg.timeout > 0 {
			cfg.toolTimeout = cfg.timeout
		} else {
			cfg.toolTimeout = 30 * time.Second
		}
	}
	// Load tools manifest if provided
	var (
		toolRegistry map[string]tools.ToolSpec
		oaiTools     []oai.Tool
	)
	var err error
	if strings.TrimSpace(cfg.toolsPath) != "" {
		toolRegistry, oaiTools, err = tools.LoadManifest(cfg.toolsPath)
		if err != nil {
			safeFprintf(stderr, "error: failed to load tools manifest: %v\n", err)
			return 1
		}
		// Validate each configured tool is available on this system before proceeding
		for name, spec := range toolRegistry {
			if len(spec.Command) == 0 {
				safeFprintf(stderr, "error: configured tool %q has no command\n", name)
				return 1
			}
			if _, lookErr := exec.LookPath(spec.Command[0]); lookErr != nil {
				safeFprintf(stderr, "error: configured tool %q is unavailable: %v (program %q)\n", name, lookErr, spec.Command[0])
				return 1
			}
		}
	}

	// Configure HTTP client with retry policy
	httpClient := oai.NewClientWithRetry(cfg.baseURL, cfg.apiKey, cfg.httpTimeout, oai.RetryPolicy{MaxRetries: cfg.httpRetries, Backoff: cfg.httpBackoff})

	var messages []oai.Message
	if strings.TrimSpace(cfg.loadMessagesPath) != "" {
		// Load messages from JSON file and validate
		data, rerr := os.ReadFile(strings.TrimSpace(cfg.loadMessagesPath))
		if rerr != nil {
			safeFprintf(stderr, "error: read load-messages file: %v\n", rerr)
			return 2
		}
		msgs, imgPrompt, err := parseSavedMessages(data)
		if err != nil {
			safeFprintf(stderr, "error: parse load-messages JSON: %v\n", err)
			return 2
		}
		messages = msgs
		if strings.TrimSpace(cfg.imagePrompt) == "" && strings.TrimSpace(imgPrompt) != "" {
			cfg.imagePrompt = strings.TrimSpace(imgPrompt)
		}
		if err := oai.ValidateMessageSequence(messages); err != nil {
			safeFprintf(stderr, "error: invalid loaded message sequence: %v\n", err)
			return 2
		}
	} else if len(cfg.initMessages) > 0 {
		// Use injected messages (tests only)
		messages = cfg.initMessages
	} else {
		// Resolve role contents from flags/files
		sys, sysErr := resolveMaybeFile(cfg.systemPrompt, cfg.systemFile)
		if sysErr != nil {
			safeFprintf(stderr, "error: %v\n", sysErr)
			return 2
		}
		prm, prmErr := resolveMaybeFile(cfg.prompt, cfg.promptFile)
		if prmErr != nil {
			safeFprintf(stderr, "error: %v\n", prmErr)
			return 2
		}
		devs, devErr := resolveDeveloperMessages(cfg.developerPrompts, cfg.developerFiles)
		if devErr != nil {
			safeFprintf(stderr, "error: %v\n", devErr)
			return 2
		}
		// Build messages honoring precedence
		var seed []oai.Message
		seed = append(seed, oai.Message{Role: oai.RoleSystem, Content: sys})
		for _, d := range devs {
			if s := strings.TrimSpace(d); s != "" {
				seed = append(seed, oai.Message{Role: oai.RoleDeveloper, Content: s})
			}
		}
		seed = append(seed, oai.Message{Role: oai.RoleUser, Content: prm})
		messages = seed
	}

	// Loop with per-request timeouts so multi-step tool calls have full budget each time.
	warnedOneKnob := false
	// Enforce a hard ceiling of 15 steps regardless of the provided value.
	effectiveMaxSteps := cfg.maxSteps
	if effectiveMaxSteps > 15 {
		effectiveMaxSteps = 15
	}
	// Pre-stage: perform a preparatory chat call and append any pre-stage tool outputs
	// to the transcript before entering the main loop. Behavior is additive only.
	// nolint below: ignore returned error intentionally to fail-open on pre-stage
	_ = func() error { //nolint:errcheck
		// Skip entirely when disabled or when tests inject initMessages
		if !cfg.prepEnabled || len(cfg.initMessages) > 0 || strings.TrimSpace(cfg.loadMessagesPath) != "" {
			return nil
		}
		// Execute pre-stage and update messages if any tool outputs were produced
		out, err := runPreStage(cfg, messages, stderr)
		if err != nil {
			// Fail-open: log one concise WARN and proceed with original messages
			safeFprintf(stderr, "WARN: pre-stage failed; skipping (reason: %s)\n", oneLine(err.Error()))
			return nil
		}
		messages = out
		return nil
	}()

	// Optional: pretty-print the final merged messages prior to the main call
	if cfg.printMessages {
		// Print a wrapper that includes metadata but omits any sensitive keys
		if b, err := json.MarshalIndent(buildMessagesWrapper(messages, strings.TrimSpace(cfg.imagePrompt)), "", "  "); err == nil {
			safeFprintln(stderr, string(b))
		}
	}

	// Optional: save the final merged messages to a JSON file before main call
	if strings.TrimSpace(cfg.saveMessagesPath) != "" {
		if err := writeSavedMessages(strings.TrimSpace(cfg.saveMessagesPath), messages, strings.TrimSpace(cfg.imagePrompt)); err != nil {
			safeFprintf(stderr, "error: write save-messages file: %v\n", err)
			return 2
		}
	}

	var step int
	for step = 0; step < effectiveMaxSteps; step++ {
		// completionCap governs optional MaxTokens on the request. It defaults to 0
		// (omitted) and will be adjusted by length backoff logic.
		completionCap := 0
		retriedForLength := false

		// Perform at most one in-step retry when finish_reason=="length".
		for {
			// Apply transcript hygiene before sending to the API when -debug is off
			hygienic := applyTranscriptHygiene(messages, cfg.debug)
			req := oai.ChatCompletionsRequest{
				Model:    cfg.model,
				Messages: hygienic,
			}
			// One-knob rule: if -top-p is set, set top_p and omit temperature; warn once.
			if cfg.topP > 0 {
				// Set top_p in the request payload
				topP := cfg.topP
				req.TopP = &topP
				if !warnedOneKnob {
					safeFprintln(stderr, "warning: -top-p is set; omitting temperature per one-knob rule")
					warnedOneKnob = true
				}
			} else {
				// Include temperature only when supported by the target model.
				if oai.SupportsTemperature(cfg.model) {
					req.Temperature = &cfg.temperature
				}
			}
			if len(oaiTools) > 0 {
				req.Tools = oaiTools
				req.ToolChoice = "auto"
			}

			// Include MaxTokens only when a positive completionCap is set.
			if completionCap > 0 {
				req.MaxTokens = completionCap
			}

			// Pre-flight validate message sequence to avoid API 400s for stray tool messages
			if err := oai.ValidateMessageSequence(req.Messages); err != nil {
				safeFprintf(stderr, "error: %v\n", err)
				return 1
			}

			// Request debug dump (no human-readable output precedes requests)
			dumpJSONIfDebug(stderr, fmt.Sprintf("chat.request step=%d", step+1), req, cfg.debug)

			// Per-call context
			callCtx, cancel := context.WithTimeout(context.Background(), cfg.httpTimeout)
			// Attempt streaming first when enabled; on unsupported, fall back
			if cfg.streamFinal {
				var streamedFinal strings.Builder
				type buffered struct{ channel, content string }
				var bufferedNonFinal []buffered
				streamErr := httpClient.StreamChat(callCtx, req, func(chunk oai.StreamChunk) error {
					// Accumulate only final channel content to stdout progressively; buffer others
					for _, ch := range chunk.Choices {
						delta := ch.Delta
						if strings.TrimSpace(delta.Content) == "" {
							continue
						}
						if strings.TrimSpace(delta.Channel) == "final" || strings.TrimSpace(delta.Channel) == "" {
							safeFprintf(stdout, "%s", delta.Content)
							streamedFinal.WriteString(delta.Content)
						} else {
							bufferedNonFinal = append(bufferedNonFinal, buffered{channel: strings.TrimSpace(delta.Channel), content: delta.Content})
						}
					}
					return nil
				})
				cancel()
				if streamErr == nil {
					// Stream finished successfully. Emit newline to finalize stdout.
					safeFprintln(stdout, "")
					if cfg.verbose {
						for _, b := range bufferedNonFinal {
							route := resolveChannelRoute(cfg, b.channel, true /*nonFinal*/)
							switch route {
							case "stdout":
								safeFprintln(stdout, strings.TrimSpace(b.content))
							case "stderr":
								safeFprintln(stderr, strings.TrimSpace(b.content))
							case "omit":
								// skip
							}
						}
					}
					return 0
				}
				// If not supported, fall through to non-streaming; otherwise treat as error
				if !strings.Contains(strings.ToLower(streamErr.Error()), "does not support streaming") {
					src := cfg.httpTimeoutSource
					if src == "" {
						src = "default"
					}
					safeFprintf(stderr, "error: chat call failed: %v (http-timeout source=%s)\n", streamErr, src)
					return 1
				}
				// Reset context for fallback after streaming attempt
				callCtx, cancel = context.WithTimeout(context.Background(), cfg.httpTimeout)
			} else {
				cancel()
				// Reset context for non-streaming path when streaming disabled
				callCtx, cancel = context.WithTimeout(context.Background(), cfg.httpTimeout)
			}

			// Fallback: non-streaming request
			resp, err := httpClient.CreateChatCompletion(callCtx, req)
			cancel()
			if err != nil {
				src := cfg.httpTimeoutSource
				if src == "" {
					src = "default"
				}
				safeFprintf(stderr, "error: chat call failed: %v (http-timeout source=%s)\n", err, src)
				return 1
			}
			if len(resp.Choices) == 0 {
				safeFprintln(stderr, "error: chat response has no choices")
				return 1
			}

			choice := resp.Choices[0]

			// Length backoff: one-time in-step retry doubling the completion cap (min 256)
			if strings.TrimSpace(choice.FinishReason) == "length" && !retriedForLength {
				prev := completionCap
				// Compute next cap: max(256, completionCap*2)
				if completionCap <= 0 {
					completionCap = 256
				} else {
					// Double with safe lower bound
					next := completionCap * 2
					if next < 256 {
						next = 256
					}
					completionCap = next
				}
				// Clamp to remaining context window before resending
				window := oai.ContextWindowForModel(cfg.model)
				estimated := oai.EstimateTokens(messages)
				completionCap = oai.ClampCompletionCap(messages, completionCap, window)
				// Emit audit entry describing the backoff decision
				oai.LogLengthBackoff(cfg.model, prev, completionCap, window, estimated)
				retriedForLength = true
				// Re-send within the same agent step without appending any messages yet
				continue
			}

			msg := choice.Message
			// Under -verbose, if the assistant returns a non-final channel, print immediately respecting routing.
			if cfg.verbose && msg.Role == oai.RoleAssistant {
				ch := strings.TrimSpace(msg.Channel)
				if ch != "final" && strings.TrimSpace(msg.Content) != "" {
					route := resolveChannelRoute(cfg, ch, true /*nonFinal*/)
					switch route {
					case "stdout":
						safeFprintln(stdout, strings.TrimSpace(msg.Content))
					case "stderr":
						safeFprintln(stderr, strings.TrimSpace(msg.Content))
					case "omit":
						// skip
					}
				}
			}

			// If the model returned tool calls and we have a registry, first append
			// the assistant message that carries tool_calls to preserve correct
			// sequencing (assistant -> tool messages -> assistant). Then append the
			// corresponding tool messages and continue the loop for the next turn.
			if len(msg.ToolCalls) > 0 && len(toolRegistry) > 0 {
				messages = append(messages, msg)
				messages = appendToolCallOutputs(messages, msg, toolRegistry, cfg)
				// Continue outer loop for another assistant response using appended tool outputs
				break
			}

			// If the model returned assistant content, handle channel-aware routing
			if msg.Role == oai.RoleAssistant && strings.TrimSpace(msg.Content) != "" {
				// Respect channel-aware printing: only print channel=="final" to stdout by default.
				ch := strings.TrimSpace(msg.Channel)
				if ch == "final" || ch == "" {
					// Determine destination per routing; default final->stdout
					dest := resolveChannelRoute(cfg, "final", false /*nonFinal*/)
					switch dest {
					case "stdout":
						safeFprintln(stdout, strings.TrimSpace(msg.Content))
					case "stderr":
						safeFprintln(stderr, strings.TrimSpace(msg.Content))
					case "omit":
						// do not print
					}
					// Dump debug response JSON after human-readable output, then exit
					dumpJSONIfDebug(stderr, fmt.Sprintf("chat.response step=%d", step+1), resp, cfg.debug)
					return 0
				} else {
					// Non-final assistant message with content: do not print to stdout by default.
					// (already printed above under -verbose)
					// Append and continue loop to get the actual final
					dumpJSONIfDebug(stderr, fmt.Sprintf("chat.response step=%d", step+1), resp, cfg.debug)
					messages = append(messages, msg)
					break
				}
			}

			// Otherwise, append message and continue (some models return assistant with empty content and no tools)
			dumpJSONIfDebug(stderr, fmt.Sprintf("chat.response step=%d", step+1), resp, cfg.debug)
			messages = append(messages, msg)
			break
		}
	}

	// If we reach here, the loop ended without printing final content.
	// Distinguish between generic termination and hitting the step cap.
	if step >= effectiveMaxSteps {
		safeFprintln(stderr, fmt.Sprintf("info: reached maximum steps (%d); needs human review", effectiveMaxSteps))
	} else {
		safeFprintln(stderr, "error: run ended without final assistant content")
	}
	return 1
}
