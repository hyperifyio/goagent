package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	toolTimeout  time.Duration // resolved per-tool timeout (final value after flags/global)
	httpRetries  int           // number of retries for HTTP
	httpBackoff  time.Duration // base backoff between retries
	temperature  float64
	debug        bool
	capabilities bool
	printConfig  bool
	// Sources for effective timeouts: "flag" | "env" | "default"
	httpTimeoutSource   string
	toolTimeoutSource   string
	globalTimeoutSource string
	// initMessages allows tests to inject a custom starting transcript to
	// exercise pre-flight validation paths (e.g., stray tool message). When
	// empty, the default [system,user] seed is used.
	initMessages []oai.Message
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
	cfg.toolTimeout = 0
	var httpSet, toolSet bool
	flag.Var(durationFlexFlag{dst: &cfg.httpTimeout, set: &httpSet}, "http-timeout", "HTTP timeout for chat completions (env OAI_HTTP_TIMEOUT; falls back to -timeout if unset)")
	flag.Var(durationFlexFlag{dst: &cfg.toolTimeout, set: &toolSet}, "tool-timeout", "Per-tool timeout (falls back to -timeout if unset)")
	flag.Float64Var(&cfg.temperature, "temp", 1.0, "Sampling temperature")
	flag.IntVar(&cfg.httpRetries, "http-retries", 2, "Number of retries for transient HTTP failures (timeouts, 429, 5xx)")
	flag.DurationVar(&cfg.httpBackoff, "http-retry-backoff", 300*time.Millisecond, "Base backoff between HTTP retry attempts (exponential)")
	flag.BoolVar(&cfg.debug, "debug", false, "Dump request/response JSON to stderr")
	flag.BoolVar(&cfg.capabilities, "capabilities", false, "Print enabled tools and exit")
	flag.BoolVar(&cfg.printConfig, "print-config", false, "Print resolved config and exit")
	ignoreError(flag.CommandLine.Parse(os.Args[1:]))

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
		safeFprintf(stderr, "effective timeouts: http-timeout=%s source=%s; tool-timeout=%s source=%s; timeout=%s source=%s\n",
			cfg.httpTimeout.String(), cfg.httpTimeoutSource,
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
	for step := 0; step < cfg.maxSteps; step++ {
		req := oai.ChatCompletionsRequest{
			Model:    cfg.model,
			Messages: messages,
		}
		// Include temperature only when supported by the target model.
		if oai.SupportsTemperature(cfg.model) {
			req.Temperature = &cfg.temperature
		}
		if len(oaiTools) > 0 {
			req.Tools = oaiTools
			req.ToolChoice = "auto"
		}

		// Pre-flight validate message sequence to avoid API 400s for stray tool messages
		if err := oai.ValidateMessageSequence(req.Messages); err != nil {
			safeFprintf(stderr, "error: %v\n", err)
			return 1
		}

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
		dumpJSONIfDebug(stderr, fmt.Sprintf("chat.response step=%d", step+1), resp, cfg.debug)

		if len(resp.Choices) == 0 {
			safeFprintln(stderr, "error: chat response has no choices")
			return 1
		}

		choice := resp.Choices[0]
		msg := choice.Message

		// If the model returned tool calls and we have a registry, first append
		// the assistant message that carries tool_calls to preserve correct
		// sequencing (assistant -> tool messages -> assistant). Then append the
		// corresponding tool messages and continue the loop for the next turn.
		if len(msg.ToolCalls) > 0 && len(toolRegistry) > 0 {
			messages = append(messages, msg)
			messages = appendToolCallOutputs(messages, msg, toolRegistry, cfg)
			// Continue loop for another assistant response using appended tool outputs
			continue
		}

		// If the model returned final assistant content, print and exit 0
		if msg.Role == oai.RoleAssistant && strings.TrimSpace(msg.Content) != "" {
			safeFprintln(stdout, strings.TrimSpace(msg.Content))
			return 0
		}

		// Otherwise, append message and continue (some models return assistant with empty content and no tools)
		messages = append(messages, msg)
	}

	safeFprintln(stderr, "error: run ended without final assistant content")
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
	b.WriteString("  -tool-timeout duration\n    Per-tool timeout (falls back to -timeout if unset)\n")
	b.WriteString("  -temp float\n    Sampling temperature (default 1.0)\n")
	b.WriteString("  -debug\n    Dump request/response JSON to stderr\n")
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
	if strings.TrimSpace(cfg.toolTimeoutSource) == "" {
		cfg.toolTimeoutSource = "default"
	}
	if strings.TrimSpace(cfg.globalTimeoutSource) == "" {
		cfg.globalTimeoutSource = "default"
	}

	// Build a minimal, stable JSON payload
	payload := map[string]string{
		"model":             cfg.model,
		"baseURL":           cfg.baseURL,
		"httpTimeout":       cfg.httpTimeout.String(),
		"httpTimeoutSource": cfg.httpTimeoutSource,
		"toolTimeout":       cfg.toolTimeout.String(),
		"toolTimeoutSource": cfg.toolTimeoutSource,
		"timeout":           cfg.timeout.String(),
		"timeoutSource":     cfg.globalTimeoutSource,
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
