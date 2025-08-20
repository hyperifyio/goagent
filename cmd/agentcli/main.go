package main

import (
    "context"
    "crypto/sha256"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "math/rand"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "strings"
    "time"

    "github.com/hyperifyio/goagent/internal/oai"
    prestage "github.com/hyperifyio/goagent/internal/oai/prestage"
    "github.com/hyperifyio/goagent/internal/tools"
)

// cliConfig moved to config.go

// flag helper types moved to flags_types.go

// getEnv and resolveAPIKeyFromEnv moved to flags_parse.go

// durationFlexFlag moved to flags_types.go
// parseFlags moved to flags_parse.go
}

// moved: main() and cliMain() now in cli_entry.go

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
		// Build messages honoring precedence:
		// System: CLI -system (if provided) else -system-file else default
		// Developer: CLI -developer / -developer-file (all, in provided order)
		// User: CLI -prompt or -prompt-file
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

// runPrepDryRun executes only the pre-stage processing (respecting -prep-enabled),
// prints the refined Harmony messages as pretty JSON to stdout, and exits with code 0 on success.
// On failure (e.g., pre-stage HTTP error), it prints a concise error to stderr and exits non-zero.
func runPrepDryRun(cfg cliConfig, stdout io.Writer, stderr io.Writer) int {
	// Build seed messages honoring the same precedence as in runAgent
	var messages []oai.Message
	if len(cfg.initMessages) > 0 {
		messages = cfg.initMessages
	} else {
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
	// Execute pre-stage unless disabled or when loading messages; on failure, exit non-zero
	if cfg.prepEnabled && len(cfg.initMessages) == 0 && strings.TrimSpace(cfg.loadMessagesPath) == "" {
		if out, err := runPreStage(cfg, messages, stderr); err == nil {
			messages = out
		} else {
			safeFprintf(stderr, "error: pre-stage failed: %v\n", err)
			return 1
		}
	}
	// Pretty-print refined messages to stdout
	if b, err := json.MarshalIndent(messages, "", "  "); err == nil {
		safeFprintln(stdout, string(b))
		return 0
	}
	// Fallback
	safeFprintln(stdout, "[]")
	return 0
}

// appendToolCallOutputs executes assistant-requested tool calls and appends their
// outputs (or deterministic error JSON) to the conversation messages.
func appendToolCallOutputs(messages []oai.Message, assistantMsg oai.Message, toolRegistry map[string]tools.ToolSpec, cfg cliConfig) []oai.Message {
	type result struct {
		msg oai.Message
	}

	results := make(chan result, len(assistantMsg.ToolCalls))

	// Launch each tool call concurrently
	for _, tc := range assistantMsg.ToolCalls {
		toolCall := tc // capture loop var
		spec, exists := toolRegistry[toolCall.Function.Name]
		if !exists {
			// Unknown tool: synthesize deterministic error JSON
			go func() {
				toolErr := map[string]string{"error": fmt.Sprintf("unknown tool: %s", toolCall.Function.Name)}
				contentBytes, mErr := json.Marshal(toolErr)
				if mErr != nil {
					contentBytes = []byte(`{"error":"internal error"}`)
				}
				results <- result{msg: oai.Message{
					Role:       oai.RoleTool,
					Name:       toolCall.Function.Name,
					ToolCallID: toolCall.ID,
					Content:    string(contentBytes),
				}}
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
			results <- result{msg: oai.Message{
				Role:       oai.RoleTool,
				Name:       toolCall.Function.Name,
				ToolCallID: toolCall.ID,
				Content:    content,
			}}
		}(spec, toolCall)
	}

	// Collect exactly one result per requested tool call
	for i := 0; i < len(assistantMsg.ToolCalls); i++ {
		r := <-results
		messages = append(messages, r.msg)
	}
	return messages
}

// dumpJSONIfDebug marshals v and prints it with a label when debug is enabled.
func dumpJSONIfDebug(w io.Writer, label string, v any, debug bool) {
	if !debug {
		return
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	safeFprintf(w, "\n--- %s ---\n%s\n", label, string(b))
}

// runPreStage performs a pre-processing call that exercises one-knob logic
// and client behavior (including parameter-recovery on 400). If the response
// includes tool_calls and a tools manifest is available, it executes those
// tool calls concurrently (mirroring main loop behavior) and appends exactly
// one tool message per id to the returned transcript. The function uses
// cfg.prepHTTPTimeout for its HTTP budget.
// runPreStage performs the preparatory chat call and optional tool execution.
// nolint:gocyclo // The flow covers caching, validation, tool policy, and is thoroughly unit/integration tested.
func runPreStage(cfg cliConfig, messages []oai.Message, stderr io.Writer) ([]oai.Message, error) {
	// Resolve pre-stage overrides with robust fallbacks so tests that construct cfg directly still work
	prepModel := func() string {
		if v := strings.TrimSpace(cfg.prepModel); v != "" {
			return v
		}
		if v := strings.TrimSpace(os.Getenv("OAI_PREP_MODEL")); v != "" {
			return v
		}
		return cfg.model
	}()
	prepBaseURL := func() string {
		if v := strings.TrimSpace(cfg.prepBaseURL); v != "" {
			return v
		}
		if v := strings.TrimSpace(os.Getenv("OAI_PREP_BASE_URL")); v != "" {
			return v
		}
		return cfg.baseURL
	}()
	prepAPIKey := func() string {
		if v := strings.TrimSpace(cfg.prepAPIKey); v != "" {
			return v
		}
		if v := strings.TrimSpace(os.Getenv("OAI_PREP_API_KEY")); v != "" {
			return v
		}
		if v := strings.TrimSpace(os.Getenv("OAI_API_KEY")); v != "" {
			return v
		}
		if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); v != "" {
			return v
		}
		return cfg.apiKey
	}()
	retries := cfg.prepHTTPRetries
	if retries <= 0 {
		retries = cfg.httpRetries
	}
	backoff := cfg.prepHTTPBackoff
	if backoff == 0 {
		backoff = cfg.httpBackoff
	}

	// Compute pre-stage sampling effective knobs for cache key
	var (
		effectiveTopP *float64
		effectiveTemp *float64
	)
	// One-knob: -prep-top-p wins and omits temperature entirely
	if cfg.prepTopP > 0 {
		tp := cfg.prepTopP
		effectiveTopP = &tp
		// temperature omitted
	} else if cfg.prepTemperatureSource == "flag" || cfg.prepTemperatureSource == "env" {
		// Explicit pre-stage temperature override via flag/env, if supported
		if oai.SupportsTemperature(prepModel) {
			t := cfg.prepTemperature
			effectiveTemp = &t
		}
	} else if strings.TrimSpace(string(cfg.prepProfile)) != "" {
		// Apply profile-derived temperature when supported
		if t, ok := oai.MapProfileToTemperature(prepModel, cfg.prepProfile); ok {
			effectiveTemp = &t
		}
	} else if oai.SupportsTemperature(prepModel) {
		// Inherit main temperature when supported and no explicit pre-stage override provided
		t := cfg.temperature
		effectiveTemp = &t
	}

	// Determine tool spec identifier for cache key
	toolSpec := func() string {
		if !cfg.prepToolsAllowExternal {
			return "builtin:fs.read_file,fs.list_dir,fs.stat,env.get,os.info"
		}
		// Prefer -prep-tools when provided; otherwise fall back to -tools
		manifest := strings.TrimSpace(cfg.prepToolsPath)
		if manifest == "" {
			manifest = strings.TrimSpace(cfg.toolsPath)
		}
		if manifest == "" {
			return "external:none"
		}
		b, err := os.ReadFile(manifest)
		if err != nil {
			// If manifest cannot be read, include the error string so key changes predictably
			return "manifest_err:" + oneLine(err.Error())
		}
		sum := sha256SumHex(b)
		return "manifest:" + sum
	}()

	// Attempt cache read unless bust requested
	if !cfg.prepCacheBust {
		if out, ok := tryReadPrepCache(prepModel, prepBaseURL, effectiveTemp, effectiveTopP, cfg.httpRetries, cfg.httpBackoff, toolSpec, messages); ok {
			return out, nil
		}
	}

	// Construct request mirroring main loop sampling rules but using -prep-top-p
	// Normalize/validate Harmony roles and assistant channels before pre-stage
	normalizedIn, normErr := oai.NormalizeHarmonyMessages(messages)
	if normErr != nil {
		safeFprintf(stderr, "error: prep invalid message role: %v\n", normErr)
		return nil, normErr
	}
	// Apply transcript hygiene before pre-stage call when -debug is off (harmless if no tool messages yet)
	// Optionally prepend a pre-stage system message when provided via flags/env
	var prepMessages []oai.Message
	if strings.TrimSpace(cfg.prepSystem) != "" || strings.TrimSpace(cfg.prepSystemFile) != "" {
		sysText, sysErr := resolveMaybeFile(strings.TrimSpace(cfg.prepSystem), strings.TrimSpace(cfg.prepSystemFile))
		if sysErr != nil {
			safeFprintf(stderr, "error: prep system read failed: %v\n", sysErr)
			return nil, sysErr
		}
		if s := strings.TrimSpace(sysText); s != "" {
			prepMessages = append(prepMessages, oai.Message{Role: oai.RoleSystem, Content: s})
		}
	}
	prepMessages = append(prepMessages, applyTranscriptHygiene(normalizedIn, cfg.debug)...)
	req := oai.ChatCompletionsRequest{
		Model:    prepModel,
		Messages: prepMessages,
	}
	// Pre-flight validate message sequence to avoid API 400s for stray tool messages
	if err := oai.ValidateMessageSequence(req.Messages); err != nil {
		safeFprintf(stderr, "error: prep invalid message sequence: %v\n", err)
		return nil, err
	}
	if effectiveTopP != nil {
		req.TopP = effectiveTopP
	} else if effectiveTemp != nil {
		req.Temperature = effectiveTemp
	}
	// Create a dedicated client honoring pre-stage timeout and normal retry policy
	httpClient := oai.NewClientWithRetry(prepBaseURL, prepAPIKey, cfg.prepHTTPTimeout, oai.RetryPolicy{MaxRetries: retries, Backoff: backoff})
	dumpJSONIfDebug(stderr, "prep.request", req, cfg.debug)
	// Tag context with audit stage so HTTP audit lines include stage: "prep"
	ctx, cancel := context.WithTimeout(oai.WithAuditStage(context.Background(), "prep"), cfg.prepHTTPTimeout)
	defer cancel()
	resp, err := httpClient.CreateChatCompletion(ctx, req)
	if err != nil {
		// Mirror main loop error style concisely; future item will add WARN+fallback behavior
		safeFprintf(stderr, "error: prep call failed: %v\n", err)
		return nil, err
	}
	dumpJSONIfDebug(stderr, "prep.response", resp, cfg.debug)

	// Under -verbose, surface non-final assistant channels from pre-stage as human-readable stderr lines
	if cfg.verbose {
		if len(resp.Choices) > 0 {
			m := resp.Choices[0].Message
			if m.Role == oai.RoleAssistant {
				ch := strings.TrimSpace(m.Channel)
				if ch != "final" && strings.TrimSpace(m.Content) != "" {
					safeFprintln(stderr, strings.TrimSpace(m.Content))
				}
			}
		}
	}

	// Parse and merge pre-stage payload into the seed messages when present
	merged := normalizedIn
	if len(resp.Choices) > 0 {
		payload := strings.TrimSpace(resp.Choices[0].Message.Content)
		if payload != "" {
			if parsed, perr := prestage.ParsePrestagePayload(payload); perr == nil {
				merged = prestage.MergePrestageIntoMessages(normalizedIn, parsed)
			}
		}
	}

	// If there are no tool calls, return merged messages
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		// Cache the merged transcript for consistency
		if err := writePrepCache(prepModel, prepBaseURL, effectiveTemp, effectiveTopP, cfg.httpRetries, cfg.httpBackoff, toolSpec, normalizedIn, merged); err != nil {
			_ = err // best-effort cache write; ignore error
		}
		return merged, nil
	}

	// Append the assistant message carrying tool_calls
	// Normalize assistant channel/token on the response message
	assistantMsg := resp.Choices[0].Message
	if norm, err := oai.NormalizeHarmonyMessages([]oai.Message{assistantMsg}); err == nil && len(norm) == 1 {
		assistantMsg = norm[0]
	}
	out := append(append([]oai.Message{}, merged...), assistantMsg)

	// Decide pre-stage tool execution policy: built-in read-only by default
	if !cfg.prepToolsAllowExternal {
		// Ignore -tools and execute only built-in read-only adapters
		out = appendPreStageBuiltinToolOutputs(out, assistantMsg, cfg)
		// Write cache
		if err := writePrepCache(prepModel, prepBaseURL, effectiveTemp, effectiveTopP, cfg.httpRetries, cfg.httpBackoff, toolSpec, normalizedIn, out); err != nil {
			_ = err // best-effort cache write; ignore error
		}
		return out, nil
	}

	// External tools allowed: require a manifest and enforce availability
	// Prefer -prep-tools when provided; otherwise use -tools
	manifest := strings.TrimSpace(cfg.prepToolsPath)
	if manifest == "" {
		manifest = strings.TrimSpace(cfg.toolsPath)
	}
	if manifest == "" {
		// No manifest; nothing to execute
		return out, nil
	}
	registry, _, lerr := tools.LoadManifest(manifest)
	if lerr != nil {
		safeFprintf(stderr, "error: failed to load tools manifest for pre-stage: %v\n", lerr)
		return nil, lerr
	}
	for name, spec := range registry {
		if len(spec.Command) == 0 {
			safeFprintf(stderr, "error: configured tool %q has no command\n", name)
			return nil, fmt.Errorf("tool %s has no command", name)
		}
		if _, lookErr := exec.LookPath(spec.Command[0]); lookErr != nil {
			safeFprintf(stderr, "error: configured tool %q is unavailable: %v (program %q)\n", name, lookErr, spec.Command[0])
			return nil, lookErr
		}
	}
	out = appendToolCallOutputs(out, assistantMsg, registry, cfg)
	if err := writePrepCache(prepModel, prepBaseURL, effectiveTemp, effectiveTopP, cfg.httpRetries, cfg.httpBackoff, toolSpec, normalizedIn, out); err != nil {
		_ = err // best-effort cache write; ignore error
	}
	return out, nil
}

// sha256SumHex returns the lowercase hex SHA-256 of b.
func sha256SumHex(b []byte) string {
	h := sha256.New()
	_, _ = h.Write(b)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// computeToolsetHash returns a stable hash of the tools manifest contents.
// When manifestPath is empty or unreadable, returns an empty string.
func computeToolsetHash(manifestPath string) string {
	path := strings.TrimSpace(manifestPath)
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return sha256SumHex(b)
}

// computeDefaultStateScope returns sha256(model + "|" + base + "|" + toolsetHash).
func computeDefaultStateScope(model string, base string, toolsetHash string) string {
	input := []byte(strings.TrimSpace(model) + "|" + strings.TrimSpace(base) + "|" + strings.TrimSpace(toolsetHash))
	return sha256SumHex(input)
}

// tryReadPrepCache attempts to load cached pre-stage output messages.
func tryReadPrepCache(model, base string, temp *float64, topP *float64, retries int, backoff time.Duration, toolSpec string, inMessages []oai.Message) ([]oai.Message, bool) {
	key := computePrepCacheKey(model, base, temp, topP, retries, backoff, toolSpec, inMessages)
	dir := filepath.Join(findRepoRoot(), ".goagent", "cache", "prep")
	path := filepath.Join(dir, key+".json")
	// TTL check based on file mtime
	fi, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	ttl := prepCacheTTL()
	if ttl > 0 {
		if fi.ModTime().Add(ttl).Before(time.Now()) {
			return nil, false
		}
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		return nil, false
	}
	var messages []oai.Message
	if jerr := json.Unmarshal(data, &messages); jerr != nil {
		return nil, false
	}
	return messages, true
}

// writePrepCache writes outMessages as JSON under the computed cache key.
func writePrepCache(model, base string, temp *float64, topP *float64, retries int, backoff time.Duration, toolSpec string, inMessages, outMessages []oai.Message) error {
	key := computePrepCacheKey(model, base, temp, topP, retries, backoff, toolSpec, inMessages)
	dir := filepath.Join(findRepoRoot(), ".goagent", "cache", "prep")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, key+".json")
	data, err := json.Marshal(outMessages)
	if err != nil {
		return err
	}
	// Atomic write: write to temp then rename
	tmp := path + ".tmp"
	if werr := os.WriteFile(tmp, data, 0o644); werr != nil {
		return werr
	}
	return os.Rename(tmp, path)
}

// computePrepCacheKey builds a deterministic key covering inputs and config.
func computePrepCacheKey(model, base string, temp *float64, topP *float64, retries int, backoff time.Duration, toolSpec string, inMessages []oai.Message) string {
	// Build a stable map for hashing
	type hashPayload struct {
		Model    string        `json:"model"`
		BaseURL  string        `json:"base_url"`
		Temp     *float64      `json:"temperature,omitempty"`
		TopP     *float64      `json:"top_p,omitempty"`
		Retries  int           `json:"retries"`
		Backoff  string        `json:"backoff"`
		ToolSpec string        `json:"tool_spec"`
		Messages []oai.Message `json:"messages"`
	}
	payload := hashPayload{
		Model:    strings.TrimSpace(model),
		BaseURL:  strings.TrimSpace(base),
		Temp:     temp,
		TopP:     topP,
		Retries:  retries,
		Backoff:  backoff.String(),
		ToolSpec: toolSpec,
		Messages: normalizeMessagesForHash(inMessages),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		// Fallback: return hash of string rendering to preserve behavior
		return sha256SumHex([]byte(fmt.Sprintf("%+v", payload)))
	}
	return sha256SumHex(b)
}

// normalizeMessagesForHash strips fields that should not affect cache equality.
func normalizeMessagesForHash(in []oai.Message) []oai.Message {
	out := make([]oai.Message, 0, len(in))
	for _, m := range in {
		nm := oai.Message{Role: strings.TrimSpace(m.Role), Content: strings.TrimSpace(m.Content)}
		// We intentionally ignore channels and tool calls in the input seed for keying
		out = append(out, nm)
	}
	return out
}

// prepCacheTTL returns the TTL for prep cache; default 10 minutes, override via GOAGENT_PREP_CACHE_TTL.
func prepCacheTTL() time.Duration {
	if v := strings.TrimSpace(os.Getenv("GOAGENT_PREP_CACHE_TTL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 10 * time.Minute
}

// findRepoRoot walks upward from CWD to locate go.mod, mirroring internal/oai moduleRoot.
func findRepoRoot() string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return "."
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

// sanitizeToolContent maps tool output and errors to a deterministic JSON string.
func sanitizeToolContent(stdout []byte, runErr error) string {
	if runErr == nil {
		// If the tool produced no output, return an empty JSON object to avoid confusing the model
		trimmed := strings.TrimSpace(string(stdout))
		if trimmed == "" {
			return "{}"
		}
		// Ensure it is one line to keep prompts compact
		return oneLine(trimmed)
	}
	// On error, return {"error":"..."}
	msg := runErr.Error()
	if errors.Is(runErr, context.DeadlineExceeded) {
		msg = "tool timed out"
	}
	// Truncate to avoid bloat
	const maxLen = 1000
	if len(msg) > maxLen {
		msg = msg[:maxLen]
	}
	// JSON-escape via marshaling
	b, mErr := json.Marshal(map[string]string{"error": msg})
	if mErr != nil {
		// Fallback to a minimal JSON on marshal error
		return "{\"error\":\"internal error\"}"
	}
	return oneLine(string(b))
}

func oneLine(s string) string {
	// Collapse newlines and tabs
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	// Collapse repeated spaces
	return strings.Join(strings.Fields(s), " ")
}

// parseSavedMessages accepts either a JSON array of oai.Message (legacy format)
// or a JSON object {"messages":[...], "image_prompt":"..."} and returns
// the parsed messages and optional image prompt.
func parseSavedMessages(data []byte) ([]oai.Message, string, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		var msgs []oai.Message
		if err := json.Unmarshal([]byte(trimmed), &msgs); err != nil {
			return nil, "", err
		}
		return msgs, "", nil
	}
	var wrapper struct {
		Messages    []oai.Message `json:"messages"`
		ImagePrompt string        `json:"image_prompt"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err != nil {
		return nil, "", err
	}
	return wrapper.Messages, strings.TrimSpace(wrapper.ImagePrompt), nil
}

// buildMessagesWrapper constructs the saved/printed JSON wrapper including
// the Harmony messages, optional image prompt, and pre-stage metadata.
func buildMessagesWrapper(messages []oai.Message, imagePrompt string) any {
	// Determine pre-stage prompt source and size using resolver.
	// Flags for pre-stage prompt are not yet implemented; this will resolve to
	// the embedded default for now, which is acceptable and deterministic.
	src, text := oai.ResolvePrepPrompt(nil, "")
	type prestageMeta struct {
		Source string `json:"source"`
		Bytes  int    `json:"bytes"`
	}
	type wrapper struct {
		Messages    []oai.Message `json:"messages"`
		ImagePrompt string        `json:"image_prompt,omitempty"`
		Prestage    prestageMeta  `json:"prestage"`
	}
	w := wrapper{
		Messages: messages,
		Prestage: prestageMeta{Source: src, Bytes: len([]byte(text))},
	}
	if strings.TrimSpace(imagePrompt) != "" {
		w.ImagePrompt = strings.TrimSpace(imagePrompt)
	}
	return w
}

// writeSavedMessages writes the wrapper JSON with messages, optional image_prompt,
// and pre-stage metadata.
func writeSavedMessages(path string, messages []oai.Message, imagePrompt string) error {
	wrapper := buildMessagesWrapper(messages, strings.TrimSpace(imagePrompt))
	b, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b, 0o644)
}

// applyTranscriptHygiene enforces transcript-size safeguards before requests.
// When debug is off, any role:"tool" message whose content exceeds 8 KiB is
// replaced with a compact JSON marker to prevent huge payloads from being sent
// upstream. Under -debug, no truncation occurs to preserve full visibility.
func applyTranscriptHygiene(in []oai.Message, debug bool) []oai.Message {
	if debug || len(in) == 0 {
		// Preserve exact transcript under -debug or when empty
		return in
	}
	const limit = 8 * 1024
	out := make([]oai.Message, 0, len(in))
	for _, m := range in {
		n := m
		if n.Role == oai.RoleTool {
			if len(n.Content) > limit {
				n.Content = `{"truncated":true,"reason":"large-tool-output"}`
			}
		}
		out = append(out, n)
	}
	return out
}

// helpRequested returns true if any canonical help token is present.
func helpRequested(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "help" {
			return true
		}
	}
	return false
}

// versionRequested returns true if any canonical version token is present.
func versionRequested(args []string) bool {
	for _, a := range args {
		if a == "--version" || a == "-version" {
			return true
		}
	}
	return false
}

// printUsage writes a comprehensive usage guide to w.
func printUsage(w io.Writer) {
	var b strings.Builder
	b.WriteString("agentcli — non-interactive CLI agent for OpenAI-compatible APIs\n\n")
	b.WriteString("Usage:\n  agentcli [flags]\n\n")
	b.WriteString("Flags (precedence: flag > env > default):\n")
	b.WriteString("  -prompt string\n    User prompt (required)\n")
	b.WriteString("  -tools string\n    Path to tools.json (optional)\n")
	b.WriteString("  -system string\n    System prompt (default \"You are a helpful, precise assistant. Use tools when strictly helpful.\")\n")
	b.WriteString("  -system-file string\n    Path to file containing system prompt ('-' for STDIN; mutually exclusive with -system)\n")
	b.WriteString("  -developer string\n    Developer message (repeatable)\n")
	b.WriteString("  -developer-file string\n    Path to file containing developer message (repeatable; '-' for STDIN)\n")
	b.WriteString("  -prompt-file string\n    Path to file containing user prompt ('-' for STDIN; mutually exclusive with -prompt)\n")
	b.WriteString("  -base-url string\n    OpenAI-compatible base URL (env OAI_BASE_URL or default https://api.openai.com/v1)\n")
	b.WriteString("  -api-key string\n    API key if required (env OAI_API_KEY; falls back to OPENAI_API_KEY)\n")
	b.WriteString("  -model string\n    Model ID (env OAI_MODEL or default oss-gpt-20b)\n")
	b.WriteString("  -max-steps int\n    Maximum reasoning/tool steps (default 8)\n")
	b.WriteString("  -timeout duration\n    [DEPRECATED] Global timeout; use -http-timeout and -tool-timeout (default 30s)\n")
	b.WriteString("  -http-timeout duration\n    HTTP timeout for chat completions (env OAI_HTTP_TIMEOUT; falls back to -timeout if unset)\n")
	b.WriteString("  -prep-http-timeout duration\n    HTTP timeout for pre-stage (env OAI_PREP_HTTP_TIMEOUT; falls back to -http-timeout if unset)\n")
	b.WriteString("  -tool-timeout duration\n    Per-tool timeout (falls back to -timeout if unset)\n")
	b.WriteString("  -http-retries int\n    Number of retries for transient HTTP failures (timeouts, 429, 5xx) (env OAI_HTTP_RETRIES; default 2)\n")
	b.WriteString("  -http-retry-backoff duration\n    Base backoff between HTTP retry attempts (exponential) (env OAI_HTTP_RETRY_BACKOFF; default 500ms)\n")
	b.WriteString("  -image-base-url string\n    Image API base URL (env OAI_IMAGE_BASE_URL; inherits -base-url if unset)\n")
	b.WriteString("  -image-model string\n    Image model ID (env OAI_IMAGE_MODEL; default gpt-image-1)\n")
	b.WriteString("  -image-api-key string\n    Image API key (env OAI_IMAGE_API_KEY; inherits -api-key if unset; falls back to OPENAI_API_KEY)\n")
	b.WriteString("  -image-http-timeout duration\n    Image HTTP timeout (env OAI_IMAGE_HTTP_TIMEOUT; inherits -http-timeout if unset)\n")
	b.WriteString("  -image-http-retries int\n    Image HTTP retries (env OAI_IMAGE_HTTP_RETRIES; inherits -http-retries if unset)\n")
	b.WriteString("  -image-http-retry-backoff duration\n    Image HTTP retry backoff (env OAI_IMAGE_HTTP_RETRY_BACKOFF; inherits -http-retry-backoff if unset)\n")
	b.WriteString("  -temp float\n    Sampling temperature (default 1.0)\n")
	b.WriteString("  -top-p float\n    Nucleus sampling probability mass (conflicts with -temp; omits temperature when set)\n")
	b.WriteString("  -prep-profile string\n    Pre-stage prompt profile (deterministic|general|creative|reasoning); sets temperature when supported (conflicts with -prep-top-p)\n")
	b.WriteString("  -prep-model string\n    Pre-stage model ID (env OAI_PREP_MODEL; inherits -model if unset)\n")
	b.WriteString("  -prep-base-url string\n    Pre-stage base URL (env OAI_PREP_BASE_URL; inherits -base-url if unset)\n")
	b.WriteString("  -prep-api-key string\n    Pre-stage API key (env OAI_PREP_API_KEY; falls back to OAI_API_KEY/OPENAI_API_KEY; inherits -api-key if unset)\n")
	b.WriteString("  -prep-http-retries int\n    Pre-stage HTTP retries (env OAI_PREP_HTTP_RETRIES; inherits -http-retries if unset)\n")
	b.WriteString("  -prep-http-retry-backoff duration\n    Pre-stage HTTP retry backoff (env OAI_PREP_HTTP_RETRY_BACKOFF; inherits -http-retry-backoff if unset)\n")
	b.WriteString("  -prep-temp float\n    Pre-stage sampling temperature (env OAI_PREP_TEMP; inherits -temp if unset; conflicts with -prep-top-p)\n")
	b.WriteString("  -prep-top-p float\n    Nucleus sampling probability mass for pre-stage (env OAI_PREP_TOP_P; conflicts with -prep-temp; omits temperature when set)\n")
	b.WriteString("  -prep-system string\n    Pre-stage system message (env OAI_PREP_SYSTEM; mutually exclusive with -prep-system-file)\n")
	b.WriteString("  -prep-system-file string\n    Path to file containing pre-stage system message ('-' for STDIN; env OAI_PREP_SYSTEM_FILE; mutually exclusive with -prep-system)\n")
	b.WriteString("  -image-n int\n    Number of images to generate (env OAI_IMAGE_N; default 1)\n")
	b.WriteString("  -image-size string\n    Image size WxH, e.g., 1024x1024 (env OAI_IMAGE_SIZE; default 1024x1024)\n")
	b.WriteString("  -image-quality string\n    Image quality: standard|hd (env OAI_IMAGE_QUALITY; default standard)\n")
	b.WriteString("  -image-style string\n    Image style: natural|vivid (env OAI_IMAGE_STYLE; default natural)\n")
	b.WriteString("  -image-response-format string\n    Image response format: url|b64_json (env OAI_IMAGE_RESPONSE_FORMAT; default url)\n")
	b.WriteString("  -image-transparent-background\n    Request transparent background when supported (env OAI_IMAGE_TRANSPARENT_BACKGROUND; default false)\n")
	b.WriteString("  -debug\n    Dump request/response JSON to stderr\n")
	b.WriteString("  -verbose\n    Also print non-final assistant channels (critic/confidence) to stderr\n")
	b.WriteString("  -quiet\n    Suppress non-final output; print only final text to stdout\n")
	b.WriteString("  -prep-tools-allow-external\n    Allow pre-stage to execute external tools from -tools (default false)\n")
	b.WriteString("  -prep-cache-bust\n    Skip pre-stage cache and force recompute\n")
	b.WriteString("  -prep-tools string\n    Path to pre-stage tools.json (optional; used only with -prep-tools-allow-external)\n")
	b.WriteString("  -prep-dry-run\n    Run pre-stage only, print refined Harmony messages to stdout, and exit 0\n")
	b.WriteString("  -state-dir string\n    Directory to persist and restore execution state across runs (env AGENTCLI_STATE_DIR)\n")
	b.WriteString("  -state-scope string\n    Optional scope key to partition saved state (env AGENTCLI_STATE_SCOPE); when empty, a default hash of model|base_url|toolset is used\n")
	b.WriteString("  -state-refine\n    Refine the loaded state bundle using -state-refine-text or -state-refine-file (requires -state-dir)\n")
	b.WriteString("  -state-refine-text string\n    Refinement input text to apply to the loaded state bundle (ignored when -state-refine-file is set; requires -state-dir)\n")
	b.WriteString("  -state-refine-file string\n    Path to file containing refinement input (wins over -state-refine-text; requires -state-dir)\n")
	b.WriteString("  -print-messages\n    Pretty-print the final merged message array to stderr before the main call\n")
	b.WriteString("  -stream-final\n    If server supports streaming, stream only assistant{channel:\"final\"} to stdout; buffer other channels for -verbose\n")
	b.WriteString("  -channel-route name=stdout|stderr|omit\n    Override default channel routing (final→stdout, critic/confidence→stderr); repeatable\n")
	b.WriteString("  -save-messages string\n    Write the final merged Harmony messages to the given JSON file and continue\n")
	b.WriteString("  -load-messages string\n    Bypass pre-stage and prompt; load Harmony messages from the given JSON file (validator-checked)\n")
	b.WriteString("  -prep-enabled\n    Enable pre-stage processing (default true; when false, skip pre-stage and proceed directly to main call)\n")
	b.WriteString("  -capabilities\n    Print enabled tools and exit\n")
	b.WriteString("  -print-config\n    Print resolved config and exit\n")
	b.WriteString("  -dry-run\n    Print intended state actions (restore/refine/save) and exit without writing state\n")
	b.WriteString("  --version | -version\n    Print version and exit\n")
	b.WriteString("\nDocs:\n")
	b.WriteString("  - Linux 5.4 sandbox compatibility and policy authoring: docs/runbooks/linux-5.4-sandbox-compatibility.md\n")
	b.WriteString("\nExamples:\n")
	b.WriteString("  # Quick start (after make build build-tools)\n")
	b.WriteString("  ./bin/agentcli -prompt \"What's the local time in Helsinki? Use get_time.\" -tools ./tools.json -debug\n\n")
	b.WriteString("  # Print capabilities (enabled tools)\n")
	b.WriteString("  ./bin/agentcli -capabilities -tools ./tools.json\n\n")
	b.WriteString("  # Show help\n")
	b.WriteString("  agentcli --help\n")
	b.WriteString("\n  # Show version\n")
	b.WriteString("  agentcli --version\n")
	safeFprintln(w, strings.TrimRight(b.String(), "\n"))
}

// Build-time variables set via -ldflags; defaults are useful for dev builds.
var (
	version   = "v0.0.0-dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// printVersion writes a concise single-line version string to stdout.
func printVersion(w io.Writer) {
	// Example: agentcli version v1.2.3 (commit abcdef1, built 2025-08-17)
	safeFprintln(w, fmt.Sprintf("agentcli version %s (commit %s, built %s)", version, shortCommit(commit), buildDate))
}

func shortCommit(c string) string {
	c = strings.TrimSpace(c)
	if len(c) > 7 {
		return c[:7]
	}
	if c == "" {
		return "unknown"
	}
	return c
}

// printResolvedConfig writes a JSON object describing resolved configuration
// (model, base URL, and timeouts with their sources) to stdout. Returns exit code 0.
func printResolvedConfig(cfg cliConfig, stdout io.Writer) int {
	// Ensure timeouts are normalized as in runAgent
	if cfg.httpTimeout <= 0 {
		if cfg.timeout > 0 {
			cfg.httpTimeout = cfg.timeout
		} else {
			cfg.httpTimeout = 90 * time.Second
		}
	}
	if cfg.toolTimeout <= 0 {
		if cfg.timeout > 0 {
			cfg.toolTimeout = cfg.timeout
		} else {
			cfg.toolTimeout = 30 * time.Second
		}
	}
	// Default sources when unset
	if strings.TrimSpace(cfg.httpTimeoutSource) == "" {
		cfg.httpTimeoutSource = "default"
	}
	if strings.TrimSpace(cfg.prepHTTPTimeoutSource) == "" {
		cfg.prepHTTPTimeoutSource = "inherit"
	}
	if strings.TrimSpace(cfg.toolTimeoutSource) == "" {
		cfg.toolTimeoutSource = "default"
	}
	if strings.TrimSpace(cfg.globalTimeoutSource) == "" {
		cfg.globalTimeoutSource = "default"
	}

	// Build a minimal, stable JSON payload
	payload := map[string]any{
		"model":                 cfg.model,
		"baseURL":               cfg.baseURL,
		"httpTimeout":           cfg.httpTimeout.String(),
		"httpTimeoutSource":     cfg.httpTimeoutSource,
		"prepHTTPTimeout":       cfg.prepHTTPTimeout.String(),
		"prepHTTPTimeoutSource": cfg.prepHTTPTimeoutSource,
		"toolTimeout":           cfg.toolTimeout.String(),
		"toolTimeoutSource":     cfg.toolTimeoutSource,
		"timeout":               cfg.timeout.String(),
		"timeoutSource":         cfg.globalTimeoutSource,
	}

	// Resolve prep-specific view for printing: env OAI_PREP_* > inherit from main
	// Use resolved cfg prep fields and sources
	prepModel, prepModelSource := cfg.prepModel, cfg.prepModelSource
	prepBase, prepBaseSource := cfg.prepBaseURL, cfg.prepBaseURLSource
	var apiKeyPresent bool
	apiKeySource := cfg.prepAPIKeySource
	if strings.TrimSpace(cfg.prepAPIKey) != "" {
		apiKeyPresent = true
	} else {
		apiKeyPresent = false
	}

	// Resolve sampling for prep: one-knob behavior with explicit overrides
	var prepTempStr, prepTempSource, prepTopPStr, prepTopPSource string
	if cfg.prepTopP > 0 {
		prepTopPStr = strconv.FormatFloat(cfg.prepTopP, 'f', -1, 64)
		prepTopPSource = cfg.prepTopPSource
		prepTempStr = "(omitted)"
		prepTempSource = "omitted:one-knob"
	} else if cfg.prepTemperatureSource == "flag" || cfg.prepTemperatureSource == "env" {
		if oai.SupportsTemperature(prepModel) {
			prepTempStr = strconv.FormatFloat(cfg.prepTemperature, 'f', -1, 64)
			prepTempSource = cfg.prepTemperatureSource
			prepTopPStr = "(omitted)"
			prepTopPSource = "inherit"
		} else {
			prepTempStr = "(omitted:unsupported)"
			prepTempSource = "unsupported"
			prepTopPStr = "(omitted)"
			prepTopPSource = "inherit"
		}
	} else {
		// Inherit main temperature when supported; else both omitted
		if oai.SupportsTemperature(prepModel) {
			prepTempStr = strconv.FormatFloat(cfg.temperature, 'f', -1, 64)
			prepTempSource = cfg.temperatureSource
			prepTopPStr = "(omitted)"
			prepTopPSource = "inherit"
		} else {
			prepTempStr = "(omitted:unsupported)"
			prepTempSource = "unsupported"
			prepTopPStr = "(omitted)"
			prepTopPSource = "inherit"
		}
	}

	// Pre-stage block
	payload["prep"] = map[string]any{
		"enabled":                cfg.prepEnabled,
		"model":                  prepModel,
		"modelSource":            prepModelSource,
		"baseURL":                prepBase,
		"baseURLSource":          prepBaseSource,
		"apiKeyPresent":          apiKeyPresent,
		"apiKeySource":           apiKeySource,
		"httpTimeout":            cfg.prepHTTPTimeout.String(),
		"httpTimeoutSource":      cfg.prepHTTPTimeoutSource,
		"httpRetries":            cfg.prepHTTPRetries,
		"httpRetriesSource":      cfg.prepHTTPRetriesSource,
		"httpRetryBackoff":       cfg.prepHTTPBackoff.String(),
		"httpRetryBackoffSource": cfg.prepHTTPBackoffSource,
		"sampling": map[string]any{
			"temperature":       prepTempStr,
			"temperatureSource": prepTempSource,
			"top_p":             prepTopPStr,
			"top_pSource":       prepTopPSource,
		},
	}
	// Image block with redacted API key
	{
		img, baseSrc, keySrc := oai.ResolveImageConfig(cfg.imageBaseURL, cfg.imageAPIKey, cfg.baseURL, cfg.apiKey)
		payload["image"] = map[string]any{
			"baseURL":                img.BaseURL,
			"baseURLSource":          baseSrc,
			"apiKey":                 oai.MaskAPIKeyLast4(img.APIKey),
			"apiKeySource":           keySrc,
			"model":                  cfg.imageModel,
			"httpTimeout":            cfg.imageHTTPTimeout.String(),
			"httpTimeoutSource":      nonEmptyOr(cfg.imageHTTPTimeoutSource, "inherit"),
			"httpRetries":            cfg.imageHTTPRetries,
			"httpRetriesSource":      nonEmptyOr(cfg.imageHTTPRetriesSource, "inherit"),
			"httpRetryBackoff":       cfg.imageHTTPBackoff.String(),
			"httpRetryBackoffSource": nonEmptyOr(cfg.imageHTTPBackoffSource, "inherit"),
			"n":                      cfg.imageN,
			"size":                   cfg.imageSize,
			"quality":                cfg.imageQuality,
			"style":                  cfg.imageStyle,
			"response_format":        cfg.imageResponseFormat,
			"transparent_background": cfg.imageTransparentBackground,
		}
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		// Fallback to a simple line to avoid surprising exits
		safeFprintln(stdout, "{}")
		return 0
	}
	safeFprintln(stdout, string(data))
	return 0
}

// printStateDryRunPlan outputs a concise plan describing intended state actions.
// It never writes to disk. Exit code 0 on success.
func printStateDryRunPlan(cfg cliConfig, stdout io.Writer, stderr io.Writer) int {
	// Normalize/expand state-dir as parseFlags would have done
	dir := strings.TrimSpace(cfg.stateDir)
	if dir != "" {
		if strings.HasPrefix(dir, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
			}
		}
		dir = filepath.Clean(dir)
	}

	// Determine action
	type plan struct {
		Action        string `json:"action"`
		StateDir      string `json:"state_dir"`
		ScopeKey      string `json:"scope_key"`
		Refine        bool   `json:"refine"`
		HasRefineText bool   `json:"has_refine_text"`
		HasRefineFile bool   `json:"has_refine_file"`
		Notes         string `json:"notes"`
	}
	p := plan{StateDir: dir, ScopeKey: strings.TrimSpace(cfg.stateScope), Refine: cfg.stateRefine, HasRefineText: strings.TrimSpace(cfg.stateRefineText) != "", HasRefineFile: strings.TrimSpace(cfg.stateRefineFile) != ""}

	if dir == "" {
		p.Action = "none"
		p.Notes = "state-dir not set; no restore/save will occur"
	} else if cfg.stateRefine || p.HasRefineText || p.HasRefineFile {
		p.Action = "refine"
		p.Notes = "would load latest bundle (if any), apply refinement, and write a new snapshot"
	} else {
		// Not refining: would attempt restore-before-prep and save afterward
		p.Action = "restore_or_save"
		p.Notes = "would attempt restore-before-prep using latest.json; on success reuse without calling pre-stage; otherwise would run pre-stage and save a new snapshot"
	}

	// Include a synthetic SHA hint to demonstrate formatting without real IO
	// This keeps output stable yet obviously a placeholder.
	hint := map[string]any{
		"sample_short_sha": fmt.Sprintf("%08x", rand.Uint32()),
	}
	out := map[string]any{
		"plan": p,
		"hint": hint,
	}
	if b, err := json.MarshalIndent(out, "", "  "); err == nil {
		safeFprintln(stdout, string(b))
		return 0
	}
	safeFprintln(stdout, "{\"plan\":{\"action\":\"unknown\"}}")
	return 0
}

// nonEmptyOr returns a when non-empty, otherwise b.
func nonEmptyOr(a, b string) string {
	if strings.TrimSpace(a) == "" {
		return b
	}
	return a
}

// writeFileAtomic writes data to path atomically by writing to a temp file
// in the same directory and then renaming it over the destination. Parent
// directories are created if missing.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// printCapabilities loads the tools manifest (if provided) and prints a concise list
// of enabled tools along with a prominent safety warning. Returns a process exit code.
func printCapabilities(cfg cliConfig, stdout io.Writer, stderr io.Writer) int {
	// If no tools path provided, report no tools and exit 0
	if strings.TrimSpace(cfg.toolsPath) == "" {
		safeFprintln(stdout, "No tools enabled (run with -tools <path to tools.json>).")
		safeFprintln(stdout, "WARNING: Enabling tools allows local process execution and may permit network access. Review tools.json carefully.")
		return 0
	}

	registry, _, err := tools.LoadManifest(cfg.toolsPath)
	if err != nil {
		safeFprintf(stderr, "error: failed to load tools manifest: %v\n", err)
		return 1
	}
	safeFprintln(stdout, "WARNING: Enabled tools can execute local binaries and may access the network. Use with caution.")
	if len(registry) == 0 {
		safeFprintln(stdout, "No tools enabled in manifest.")
		return 0
	}
	safeFprintln(stdout, "Capabilities (enabled tools):")
	// Stable ordering: lexicographic by name for deterministic output
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	// simple insertion sort to avoid importing sort just for one call; keep dependencies minimal
	for i := 1; i < len(names); i++ {
		j := i
		for j > 0 && names[j] < names[j-1] {
			names[j], names[j-1] = names[j-1], names[j]
			j--
		}
	}
	for _, name := range names {
		spec := registry[name]
		desc := strings.TrimSpace(spec.Description)
		if desc == "" {
			desc = "(no description)"
		}
		// Add an explicit per-tool warning for img_create since it performs outbound network calls
		// and can write image files to the repository when configured to save.
		if name == "img_create" {
			desc = desc + " [WARNING: makes outbound network calls and can save files]"
		}
		safeFprintf(stdout, "- %s: %s\n", name, desc)
	}
	return 0
}

// (Deprecated) durationFlexValue was used for an earlier flag implementation.
// It is intentionally removed to avoid unused-code lints; parsing is handled
// by durationFlexFlag and parseDurationFlexible.

// parseDurationFlexible parses a duration allowing either Go duration syntax
// or a plain integer representing seconds.
func parseDurationFlexible(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	// Accept plain integer seconds
	allDigits := true
	for _, r := range s {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, err
		}
		if n <= 0 {
			return 0, fmt.Errorf("duration seconds must be > 0")
		}
		return time.Duration(n) * time.Second, nil
	}
	return 0, fmt.Errorf("invalid duration: %q", raw)
}

// ignoreError is used to explicitly acknowledge and ignore expected errors
// in places where failure is handled via alternative control flow (e.g.,
// we parse flags with ContinueOnError and then return exit codes). This
// satisfies linters that require checking error returns while keeping the
// intended behavior unchanged.
func ignoreError(_ error) {}

// safeFprintln writes a line to w and intentionally ignores write errors.
// This encapsulation makes the intent explicit and satisfies errcheck.
func safeFprintln(w io.Writer, a ...any) {
	if _, err := fmt.Fprintln(w, a...); err != nil {
		return
	}
}

// safeFprintf writes formatted text to w and intentionally ignores write errors.
// This encapsulation makes the intent explicit and satisfies errcheck.
func safeFprintf(w io.Writer, format string, a ...any) {
	if _, err := fmt.Fprintf(w, format, a...); err != nil {
		return
	}
}

// resolveChannelRoute returns the destination for a given assistant channel.
// Defaults: final→stdout; non-final (critic/confidence)→stderr. Unknown/empty
// channels default to final behavior. When an override is provided via
// -channel-route, it takes precedence.
func resolveChannelRoute(cfg cliConfig, channel string, nonFinal bool) string {
	ch := strings.TrimSpace(channel)
	if ch == "" {
		ch = "final"
	}
	if cfg.channelRoutes != nil {
		if dest, ok := cfg.channelRoutes[ch]; ok {
			return dest
		}
	}
	if ch == "final" {
		return "stdout"
	}
	// Default non-final route
	return "stderr"
}

// stringSliceFlag moved to flags_types.go

// resolveMaybeFile returns the effective content from either an inline string
// or a file path when provided. When filePath is "-", it reads from STDIN.
// If filePath is non-empty, it takes precedence over inline.
func resolveMaybeFile(inline string, filePath string) (string, error) {
	f := strings.TrimSpace(filePath)
	if f == "" {
		return inline, nil
	}
	if f == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read STDIN: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", f, err)
	}
	return string(b), nil
}

// resolveDeveloperMessages aggregates developer messages from repeatable flags
// and files. Files are read in the order provided; "-" reads from STDIN.
func resolveDeveloperMessages(inlines []string, files []string) ([]string, error) {
	var out []string
	for _, f := range files {
		s, err := resolveMaybeFile("", f)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	out = append(out, inlines...)
	return out, nil
}
