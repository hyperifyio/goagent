package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hyperifyio/goagent/internal/oai"
	"github.com/hyperifyio/goagent/internal/tools"
)

// cliConfig holds user-supplied configuration resolved from flags and env.
type cliConfig struct {
	prompt       string
	toolsPath    string
	systemPrompt string
	baseURL      string
	apiKey       string
	model        string
	maxSteps     int
	timeout      time.Duration // deprecated global timeout; kept for backward compatibility
	httpTimeout  time.Duration // resolved HTTP timeout (final value after env/flags/global)
    prepHTTPTimeout time.Duration // resolved pre-stage HTTP timeout (inherits from http-timeout)
	toolTimeout  time.Duration // resolved per-tool timeout (final value after flags/global)
	httpRetries  int           // number of retries for HTTP
	httpBackoff  time.Duration // base backoff between retries
	temperature  float64
	topP         float64
    prepTopP     float64
	debug        bool
    verbose      bool
    quiet        bool
    // Pre-stage cache controls
    prepCacheBust bool // when true, bypass pre-stage cache for this run
	capabilities bool
	printConfig  bool
    // Pre-stage tool policy
    prepToolsAllowExternal bool // when false, pre-stage uses built-in read-only tools and ignores -tools
	// Sources for effective timeouts: "flag" | "env" | "default"
	httpTimeoutSource   string
    prepHTTPTimeoutSource string
	toolTimeoutSource   string
	globalTimeoutSource string
    // Sources for sampling knobs
    temperatureSource string // "flag" | "env" | "default"
    prepTopPSource    string // "flag" | "inherit"
	// initMessages allows tests to inject a custom starting transcript to
	// exercise pre-flight validation paths (e.g., stray tool message). When
	// empty, the default [system,user] seed is used.
	initMessages []oai.Message
}

// float64FlexFlag wires a float64 destination and records if it was set via flag.
type float64FlexFlag struct {
	dst *float64
	set *bool
}

func (f *float64FlexFlag) String() string {
	if f == nil || f.dst == nil {
		return ""
	}
	return strconv.FormatFloat(*f.dst, 'f', -1, 64)
}

func (f *float64FlexFlag) Set(s string) error {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return err
	}
	if f.dst != nil {
		*f.dst = v
	}
	if f.set != nil {
		*f.set = true
	}
	return nil
}

func getEnv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

// resolveAPIKeyFromEnv returns the API key using canonical and legacy env vars.
// Precedence: OAI_API_KEY > OPENAI_API_KEY > "".
func resolveAPIKeyFromEnv() string {
	if v := os.Getenv("OAI_API_KEY"); strings.TrimSpace(v) != "" {
		return v
	}
	if v := os.Getenv("OPENAI_API_KEY"); strings.TrimSpace(v) != "" {
		return v
	}
	return ""
}

// durationFlexFlag wires a duration destination and records if it was set via flag.
type durationFlexFlag struct {
	dst *time.Duration
	set *bool
}

func (f durationFlexFlag) String() string {
	if f.dst == nil {
		return ""
	}
	return f.dst.String()
}

func (f durationFlexFlag) Set(s string) error {
	d, err := parseDurationFlexible(s)
	if err != nil {
		return err
	}
	*f.dst = d
	if f.set != nil {
		*f.set = true
	}
	return nil
}

func parseFlags() (cliConfig, int) {
	var cfg cliConfig

	// Reset default FlagSet to allow re-entrant parsing in tests.
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	// Silence automatic usage/errors; we handle messaging ourselves.
	flag.CommandLine.SetOutput(io.Discard)

	defaultSystem := "You are a helpful, precise assistant. Use tools when strictly helpful."
	defaultBase := getEnv("OAI_BASE_URL", "https://api.openai.com/v1")
	defaultModel := getEnv("OAI_MODEL", "oss-gpt-20b")
	// API key resolves from env with fallback for compatibility
	defaultKey := resolveAPIKeyFromEnv()

	flag.StringVar(&cfg.prompt, "prompt", "", "User prompt (required)")
	flag.StringVar(&cfg.toolsPath, "tools", "", "Path to tools.json (optional)")
	flag.StringVar(&cfg.systemPrompt, "system", defaultSystem, "System prompt")
	flag.StringVar(&cfg.baseURL, "base-url", defaultBase, "OpenAI-compatible base URL")
	flag.StringVar(&cfg.apiKey, "api-key", defaultKey, "API key if required (env OAI_API_KEY; falls back to OPENAI_API_KEY)")
	flag.StringVar(&cfg.model, "model", defaultModel, "Model ID")
	flag.IntVar(&cfg.maxSteps, "max-steps", 8, "Maximum reasoning/tool steps")
	// Deprecated global timeout retained as a fallback if the split timeouts are not provided
	// Accept plain seconds (e.g., 300 => 300s) in addition to Go duration strings.
	cfg.timeout = 30 * time.Second
	var globalSet bool
	flag.Var(durationFlexFlag{dst: &cfg.timeout, set: &globalSet}, "timeout", "[DEPRECATED] Global timeout; use -http-timeout and -tool-timeout")
	// New split timeouts (default to 0; accept plain seconds or Go duration strings)
    cfg.httpTimeout = 0
    cfg.prepHTTPTimeout = 0
	cfg.toolTimeout = 0
    var httpSet, toolSet bool
    var prepHTTPSet bool
	flag.Var(durationFlexFlag{dst: &cfg.httpTimeout, set: &httpSet}, "http-timeout", "HTTP timeout for chat completions (env OAI_HTTP_TIMEOUT; falls back to -timeout if unset)")
    flag.Var(durationFlexFlag{dst: &cfg.prepHTTPTimeout, set: &prepHTTPSet}, "prep-http-timeout", "HTTP timeout for pre-stage (env OAI_PREP_HTTP_TIMEOUT; falls back to -http-timeout if unset)")
	flag.Var(durationFlexFlag{dst: &cfg.toolTimeout, set: &toolSet}, "tool-timeout", "Per-tool timeout (falls back to -timeout if unset)")
	// Use a flexible float flag to detect whether -temp was explicitly set
	var tempSet bool
	var _ flag.Value = (*float64FlexFlag)(nil)
	(func() {
		f := &float64FlexFlag{dst: &cfg.temperature, set: &tempSet}
		// initialize default before registering
		cfg.temperature = 1.0
		flag.CommandLine.Var(f, "temp", "Sampling temperature")
	})()

	// Nucleus sampling (one-knob with temperature). Not yet sent to API; used to gate temperature.
	flag.Float64Var(&cfg.topP, "top-p", 0, "Nucleus sampling probability mass (conflicts with temperature)")
    // Pre-stage nucleus sampling (one-knob with temperature for pre-stage)
    flag.Float64Var(&cfg.prepTopP, "prep-top-p", 0, "Nucleus sampling probability mass for pre-stage (conflicts with temperature)")
	flag.IntVar(&cfg.httpRetries, "http-retries", 2, "Number of retries for transient HTTP failures (timeouts, 429, 5xx)")
	flag.DurationVar(&cfg.httpBackoff, "http-retry-backoff", 300*time.Millisecond, "Base backoff between HTTP retry attempts (exponential)")
    flag.BoolVar(&cfg.debug, "debug", false, "Dump request/response JSON to stderr")
    flag.BoolVar(&cfg.verbose, "verbose", false, "Also print non-final assistant channels (critic/confidence) to stderr")
    flag.BoolVar(&cfg.quiet, "quiet", false, "Suppress non-final output; print only final text to stdout")
    flag.BoolVar(&cfg.prepToolsAllowExternal, "prep-tools-allow-external", false, "Allow pre-stage to execute external tools from -tools; when false, pre-stage is limited to built-in read-only tools")
    flag.BoolVar(&cfg.prepCacheBust, "prep-cache-bust", false, "Skip pre-stage cache and force recompute")
	flag.BoolVar(&cfg.capabilities, "capabilities", false, "Print enabled tools and exit")
	flag.BoolVar(&cfg.printConfig, "print-config", false, "Print resolved config and exit")
	ignoreError(flag.CommandLine.Parse(os.Args[1:]))

	// Resolve temperature precedence: flag > env (LLM_TEMPERATURE) > config file (not implemented) > default 1.0
    if tempSet {
        cfg.temperatureSource = "flag"
    } else {
        if v := strings.TrimSpace(os.Getenv("LLM_TEMPERATURE")); v != "" {
            if parsed, err := strconv.ParseFloat(v, 64); err == nil {
                cfg.temperature = parsed
                cfg.temperatureSource = "env"
            }
        }
        // Config file precedence placeholder: no-op (no config file mechanism yet)
        if cfg.temperatureSource == "" {
            cfg.temperatureSource = "default"
        }
    }

    // Resolve split timeouts with precedence: flag > env (HTTP only) > legacy -timeout > sane default
	// HTTP timeout: env OAI_HTTP_TIMEOUT supported
	httpEnvUsed := false
	if cfg.httpTimeout <= 0 {
		if v := strings.TrimSpace(os.Getenv("OAI_HTTP_TIMEOUT")); v != "" {
			if d, err := parseDurationFlexible(v); err == nil && d > 0 {
				cfg.httpTimeout = d
				httpEnvUsed = true
			}
		}
	}
	if cfg.httpTimeout <= 0 {
		if cfg.timeout > 0 {
			cfg.httpTimeout = cfg.timeout
		} else {
			cfg.httpTimeout = 90 * time.Second // sane default between 60–120s
		}
	}

    // Pre-stage HTTP timeout: precedence flag > env OAI_PREP_HTTP_TIMEOUT > http-timeout > default
    prepEnvUsed := false
    if cfg.prepHTTPTimeout <= 0 {
        if v := strings.TrimSpace(os.Getenv("OAI_PREP_HTTP_TIMEOUT")); v != "" {
            if d, err := parseDurationFlexible(v); err == nil && d > 0 {
                cfg.prepHTTPTimeout = d
                prepEnvUsed = true
            }
        }
    }
    if cfg.prepHTTPTimeout <= 0 {
        if cfg.httpTimeout > 0 {
            cfg.prepHTTPTimeout = cfg.httpTimeout
        } else {
            cfg.prepHTTPTimeout = 90 * time.Second
        }
    }

	// Tool timeout: no env per checklist; fallback to legacy -timeout or 30s default
	if cfg.toolTimeout <= 0 {
		if cfg.timeout > 0 {
			cfg.toolTimeout = cfg.timeout
		} else {
			cfg.toolTimeout = 30 * time.Second
		}
	}

	// Set source labels
	if httpSet {
		cfg.httpTimeoutSource = "flag"
	} else if httpEnvUsed {
		cfg.httpTimeoutSource = "env"
	} else {
		cfg.httpTimeoutSource = "default"
	}
    if prepHTTPSet {
        cfg.prepHTTPTimeoutSource = "flag"
    } else if prepEnvUsed {
        cfg.prepHTTPTimeoutSource = "env"
    } else {
        // inherits http-timeout or default
        cfg.prepHTTPTimeoutSource = "inherit"
    }
	if toolSet {
		cfg.toolTimeoutSource = "flag"
	} else {
		cfg.toolTimeoutSource = "default"
	}
	if globalSet {
		cfg.globalTimeoutSource = "flag"
	} else {
		cfg.globalTimeoutSource = "default"
	}

    if !cfg.capabilities && !cfg.printConfig && strings.TrimSpace(cfg.prompt) == "" {
		return cfg, 2 // CLI misuse
	}
    // Prep top_p source labeling for config dump
    if cfg.prepTopP > 0 {
        cfg.prepTopPSource = "flag"
    } else {
        cfg.prepTopPSource = "inherit"
    }
	return cfg, 0
}

func main() {
	os.Exit(cliMain(os.Args[1:], os.Stdout, os.Stderr))
}

// cliMain is a testable entrypoint for the CLI. It accepts argv (excluding program name)
// and writers for stdout/stderr, returns the intended process exit code, and performs
// no global side effects beyond temporarily setting os.Args for flag parsing.
func cliMain(args []string, stdout io.Writer, stderr io.Writer) int {
	// Handle help flags prior to any parsing/validation or side effects
	if helpRequested(args) {
		printUsage(stdout)
		return 0
	}
	// Handle version flags prior to parsing/validation
	if versionRequested(args) {
		printVersion(stdout)
		return 0
	}

	// Temporarily set os.Args so parseFlags() (which reads os.Args) sees our args
	origArgs := os.Args
	os.Args = append([]string{origArgs[0]}, args...)
	defer func() { os.Args = origArgs }()

	cfg, exitOn := parseFlags()
	if exitOn != 0 {
		safeFprintln(stderr, "error: -prompt is required")
		// Also print usage synopsis for guidance on missing required flag
		printUsage(stderr)
		return exitOn
	}
	if cfg.printConfig {
		return printResolvedConfig(cfg, stdout)
	}
	if cfg.capabilities {
		return printCapabilities(cfg, stdout, stderr)
	}
	return runAgent(cfg, stdout, stderr)
}

// runAgent executes the non-interactive agent loop and returns a process exit code.
// nolint:gocyclo // Orchestrates the agent loop; complexity is acceptable and covered by tests.
func runAgent(cfg cliConfig, stdout io.Writer, stderr io.Writer) int {
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
	if len(cfg.initMessages) > 0 {
		// Use injected messages (tests only)
		messages = cfg.initMessages
	} else {
		messages = []oai.Message{
			{Role: oai.RoleSystem, Content: cfg.systemPrompt},
			{Role: oai.RoleUser, Content: cfg.prompt},
		}
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
    if err := func() error {
        // Only run pre-stage when not using injected initMessages (tests may target specific flows)
        if len(cfg.initMessages) > 0 {
            return nil
        }
        // Execute pre-stage and update messages if any tool outputs were produced
        out, err := runPreStage(cfg, messages, stderr)
        if err != nil {
            // On error, keep existing messages; later checklist items will implement fail-open
            return nil
        }
        messages = out
        return nil
    }(); err != nil {
        // no-op; pre-stage best-effort for now
    }

    var step int
    for step = 0; step < effectiveMaxSteps; step++ {
        // completionCap governs optional MaxTokens on the request. It defaults to 0
        // (omitted) and will be adjusted by length backoff logic.
        completionCap := 0
        retriedForLength := false

        // Perform at most one in-step retry when finish_reason=="length".
        for {
            req := oai.ChatCompletionsRequest{
                Model:    cfg.model,
                Messages: messages,
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
            resp, err := httpClient.CreateChatCompletion(callCtx, req)
            cancel()
            if err != nil {
                // Append source for http-timeout to aid diagnostics
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
            // Under -verbose, if the assistant returns a non-final channel, print to stderr immediately.
            if cfg.verbose && msg.Role == oai.RoleAssistant {
                ch := strings.TrimSpace(msg.Channel)
                if ch != "final" && strings.TrimSpace(msg.Content) != "" {
                    safeFprintln(stderr, strings.TrimSpace(msg.Content))
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
                    if !cfg.quiet {
                        safeFprintln(stdout, strings.TrimSpace(msg.Content))
                    } else {
                        // quiet still prints the final text (requirement); so print regardless of quiet.
                        safeFprintln(stdout, strings.TrimSpace(msg.Content))
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
func runPreStage(cfg cliConfig, messages []oai.Message, stderr io.Writer) ([]oai.Message, error) {
    // Resolve pre-stage overrides from env: OAI_PREP_* > inherit main
    prepModel := func() string {
        if v := strings.TrimSpace(os.Getenv("OAI_PREP_MODEL")); v != "" { return v }
        return cfg.model
    }()
    prepBaseURL := func() string {
        if v := strings.TrimSpace(os.Getenv("OAI_PREP_BASE_URL")); v != "" { return v }
        return cfg.baseURL
    }()
    prepAPIKey := func() string {
        if v := strings.TrimSpace(os.Getenv("OAI_PREP_API_KEY")); v != "" { return v }
        if v := strings.TrimSpace(os.Getenv("OAI_API_KEY")); v != "" { return v }
        if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); v != "" { return v }
        return cfg.apiKey
    }()

    // Compute pre-stage sampling effective knobs for cache key
    var (
        effectiveTopP *float64
        effectiveTemp *float64
    )
    if cfg.prepTopP > 0 {
        tp := cfg.prepTopP
        effectiveTopP = &tp
        // temperature omitted
    } else if oai.SupportsTemperature(prepModel) {
        t := cfg.temperature
        effectiveTemp = &t
    }

    // Determine tool spec identifier for cache key
    toolSpec := func() string {
        if !cfg.prepToolsAllowExternal {
            return "builtin:fs.read_file,fs.list_dir,fs.stat,env.get,os.info"
        }
        if strings.TrimSpace(cfg.toolsPath) == "" {
            return "external:none"
        }
        b, err := os.ReadFile(cfg.toolsPath)
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
    req := oai.ChatCompletionsRequest{
        Model:    prepModel,
        Messages: messages,
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
    httpClient := oai.NewClientWithRetry(prepBaseURL, prepAPIKey, cfg.prepHTTPTimeout, oai.RetryPolicy{MaxRetries: cfg.httpRetries, Backoff: cfg.httpBackoff})
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

    // If there are no tool calls, return messages unchanged
    if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
        // Cache the unchanged transcript as well for consistency
        _ = writePrepCache(prepModel, prepBaseURL, effectiveTemp, effectiveTopP, cfg.httpRetries, cfg.httpBackoff, toolSpec, messages, messages)
        return messages, nil
    }

    // Append the assistant message carrying tool_calls
    assistantMsg := resp.Choices[0].Message
    out := append(append([]oai.Message{}, messages...), assistantMsg)

    // Decide pre-stage tool execution policy: built-in read-only by default
    if !cfg.prepToolsAllowExternal {
        // Ignore -tools and execute only built-in read-only adapters
        out = appendPreStageBuiltinToolOutputs(out, assistantMsg, cfg)
        // Write cache
        _ = writePrepCache(prepModel, prepBaseURL, effectiveTemp, effectiveTopP, cfg.httpRetries, cfg.httpBackoff, toolSpec, messages, out)
        return out, nil
    }

    // External tools allowed: require a manifest and enforce availability
    if strings.TrimSpace(cfg.toolsPath) == "" {
        // No manifest; nothing to execute
        return out, nil
    }
    registry, _, lerr := tools.LoadManifest(cfg.toolsPath)
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
    _ = writePrepCache(prepModel, prepBaseURL, effectiveTemp, effectiveTopP, cfg.httpRetries, cfg.httpBackoff, toolSpec, messages, out)
    return out, nil
}

// sha256SumHex returns the lowercase hex SHA-256 of b.
func sha256SumHex(b []byte) string {
    h := sha256.New()
    _, _ = h.Write(b)
    return fmt.Sprintf("%x", h.Sum(nil))
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
        Model    string         `json:"model"`
        BaseURL  string         `json:"base_url"`
        Temp     *float64       `json:"temperature,omitempty"`
        TopP     *float64       `json:"top_p,omitempty"`
        Retries  int            `json:"retries"`
        Backoff  string         `json:"backoff"`
        ToolSpec string         `json:"tool_spec"`
        Messages []oai.Message  `json:"messages"`
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
    b, _ := json.Marshal(payload)
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
    if err != nil || cwd == "" { return "." }
    dir := cwd
    for {
        if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil { return dir }
        parent := filepath.Dir(dir)
        if parent == dir { return cwd }
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
	b.WriteString("  -base-url string\n    OpenAI-compatible base URL (env OAI_BASE_URL or default https://api.openai.com/v1)\n")
	b.WriteString("  -api-key string\n    API key if required (env OAI_API_KEY; falls back to OPENAI_API_KEY)\n")
	b.WriteString("  -model string\n    Model ID (env OAI_MODEL or default oss-gpt-20b)\n")
	b.WriteString("  -max-steps int\n    Maximum reasoning/tool steps (default 8)\n")
	b.WriteString("  -timeout duration\n    [DEPRECATED] Global timeout; use -http-timeout and -tool-timeout (default 30s)\n")
	b.WriteString("  -http-timeout duration\n    HTTP timeout for chat completions (env OAI_HTTP_TIMEOUT; falls back to -timeout if unset)\n")
    b.WriteString("  -prep-http-timeout duration\n    HTTP timeout for pre-stage (env OAI_PREP_HTTP_TIMEOUT; falls back to -http-timeout if unset)\n")
	b.WriteString("  -tool-timeout duration\n    Per-tool timeout (falls back to -timeout if unset)\n")
	b.WriteString("  -temp float\n    Sampling temperature (default 1.0)\n")
	b.WriteString("  -top-p float\n    Nucleus sampling probability mass (conflicts with -temp; omits temperature when set)\n")
    b.WriteString("  -debug\n    Dump request/response JSON to stderr\n")
    b.WriteString("  -verbose\n    Also print non-final assistant channels (critic/confidence) to stderr\n")
    b.WriteString("  -quiet\n    Suppress non-final output; print only final text to stdout\n")
    b.WriteString("  -prep-tools-allow-external\n    Allow pre-stage to execute external tools from -tools (default false)\n")
    b.WriteString("  -prep-cache-bust\n    Skip pre-stage cache and force recompute\n")
	b.WriteString("  -capabilities\n    Print enabled tools and exit\n")
	b.WriteString("  -print-config\n    Print resolved config and exit\n")
	b.WriteString("  --version | -version\n    Print version and exit\n")
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
		"model":             cfg.model,
		"baseURL":           cfg.baseURL,
		"httpTimeout":       cfg.httpTimeout.String(),
		"httpTimeoutSource": cfg.httpTimeoutSource,
        "prepHTTPTimeout":       cfg.prepHTTPTimeout.String(),
        "prepHTTPTimeoutSource": cfg.prepHTTPTimeoutSource,
		"toolTimeout":       cfg.toolTimeout.String(),
		"toolTimeoutSource": cfg.toolTimeoutSource,
		"timeout":           cfg.timeout.String(),
		"timeoutSource":     cfg.globalTimeoutSource,
	}

    // Resolve prep-specific view for printing: env OAI_PREP_* > inherit from main
    resolvePrepAPIKey := func() (present bool, source string) {
        if v := strings.TrimSpace(os.Getenv("OAI_PREP_API_KEY")); v != "" {
            return true, "env:OAI_PREP_API_KEY"
        }
        if v := strings.TrimSpace(os.Getenv("OAI_API_KEY")); v != "" {
            return true, "env:OAI_API_KEY"
        }
        if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); v != "" {
            return true, "env:OPENAI_API_KEY"
        }
        return false, "empty"
    }
    prepModel, prepModelSource := func() (string, string) {
        if v := strings.TrimSpace(os.Getenv("OAI_PREP_MODEL")); v != "" {
            return v, "env"
        }
        return cfg.model, "inherit"
    }()
    prepBase, prepBaseSource := func() (string, string) {
        if v := strings.TrimSpace(os.Getenv("OAI_PREP_BASE_URL")); v != "" {
            return v, "env"
        }
        return cfg.baseURL, "inherit"
    }()
    apiKeyPresent, apiKeySource := resolvePrepAPIKey()

    // Resolve sampling for prep: one-knob behavior
    var prepTempStr, prepTempSource, prepTopPStr, prepTopPSource string
    if cfg.prepTopP > 0 {
        prepTopPStr = strconv.FormatFloat(cfg.prepTopP, 'f', -1, 64)
        prepTopPSource = cfg.prepTopPSource
        prepTempStr = "(omitted)"
        prepTempSource = "omitted:one-knob"
    } else {
        // Use temperature when supported; else both omitted
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
        "model":               prepModel,
        "modelSource":         prepModelSource,
        "baseURL":             prepBase,
        "baseURLSource":       prepBaseSource,
        "apiKeyPresent":       apiKeyPresent,
        "apiKeySource":        apiKeySource,
        "httpTimeout":         cfg.prepHTTPTimeout.String(),
        "httpTimeoutSource":   cfg.prepHTTPTimeoutSource,
        "httpRetries":         cfg.httpRetries,
        "httpRetriesSource":   "inherit",
        "httpRetryBackoff":    cfg.httpBackoff.String(),
        "httpRetryBackoffSource": "inherit",
        "sampling": map[string]any{
            "temperature":       prepTempStr,
            "temperatureSource": prepTempSource,
            "top_p":             prepTopPStr,
            "top_pSource":       prepTopPSource,
        },
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
