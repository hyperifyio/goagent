package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hyperifyio/goagent/internal/oai"
)

// cliConfig holds user-supplied configuration resolved from flags and env.
type cliConfig struct {
	prompt string
	// Role inputs: developer and files
	developerPrompts []string
	developerFiles   []string
	systemFile       string
	promptFile       string
	// Pre-stage specific system message inputs
	prepSystem      string
	prepSystemFile  string
	toolsPath       string
	systemPrompt    string
	baseURL         string
	apiKey          string
	model           string
	maxSteps        int
	timeout         time.Duration // deprecated global timeout; kept for backward compatibility
	httpTimeout     time.Duration // resolved HTTP timeout (final value after env/flags/global)
	prepHTTPTimeout time.Duration // resolved pre-stage HTTP timeout (inherits from http-timeout)
	toolTimeout     time.Duration // resolved per-tool timeout (final value after flags/global)
	httpRetries     int           // number of retries for HTTP
	httpBackoff     time.Duration // base backoff between retries
	temperature     float64
	topP            float64
	prepTopP        float64
	// Pre-stage explicit temperature override and its source
	prepTemperature       float64
	prepTemperatureSource string // "flag" | "env" | "inherit"
	// Pre-stage prompt profile controlling effective temperature when supported
	prepProfile oai.PromptProfile
	debug       bool
	verbose     bool
	quiet       bool
	// Pre-stage cache controls
	prepCacheBust bool // when true, bypass pre-stage cache for this run
	// Pre-stage master switch
	prepEnabled bool // when false, completely skip pre-stage
	// Tracks whether -prep-enabled was explicitly provided by the user
	prepEnabledSet bool
	capabilities   bool
	printConfig    bool
	// Dry-run planning for state persistence actions
	dryRun bool
	// State persistence
	stateDir string
	// Optional partition key for persisted state; when empty we compute a default
	// as sha256(model_id + "|" + base_url + "|" + toolset_hash)
	stateScope string
	// Refinement controls
	stateRefine     bool   // when true, perform refinement of a loaded state bundle
	stateRefineText string // optional refinement text input
	stateRefineFile string // optional refinement file path; wins over text when both provided
	// Pre-stage tool policy
	prepToolsAllowExternal bool // when false, pre-stage uses built-in read-only tools and ignores -tools
	// Optional pre-stage-specific tools manifest path; when set and external tools are allowed,
	// the pre-stage uses this manifest instead of -tools
	prepToolsPath string
	// Sources for effective timeouts: "flag" | "env" | "default"
	httpTimeoutSource     string
	prepHTTPTimeoutSource string
	toolTimeoutSource     string
	globalTimeoutSource   string
	// Sources for sampling knobs
	temperatureSource string // "flag" | "env" | "default"
	prepTopPSource    string // "flag" | "env" | "inherit"
	// Pre-stage explicit overrides
	prepModel       string
	prepBaseURL     string
	prepAPIKey      string
	prepHTTPRetries int
	prepHTTPBackoff time.Duration
	// Sources for prep overrides
	prepModelSource       string // "flag" | "env" | "inherit"
	prepBaseURLSource     string // "flag" | "env" | "inherit"
	prepAPIKeySource      string // "flag" | "env:OAI_PREP_API_KEY|env:OAI_API_KEY|env:OPENAI_API_KEY" | "inherit|empty"
	prepHTTPRetriesSource string // "flag" | "env" | "inherit"
	prepHTTPBackoffSource string // "flag" | "env" | "inherit"
	// Image API overrides and sources
	imageBaseURL       string
	imageAPIKey        string
	imageBaseURLSource string // "flag" | "env" | "inherit"
	imageAPIKeySource  string // "flag" | "env|env:OPENAI_API_KEY" | "inherit|empty"
	// Image HTTP behavior
	imageHTTPTimeout       time.Duration
	imageHTTPRetries       int
	imageHTTPBackoff       time.Duration
	imageHTTPTimeoutSource string // "flag" | "env" | "inherit"
	imageHTTPRetriesSource string // "flag" | "env" | "inherit"
	imageHTTPBackoffSource string // "flag" | "env" | "inherit"
	// Image request parameter pass-throughs
	imageModel                 string
	imageN                     int
	imageSize                  string
	imageQuality               string // standard|hd
	imageStyle                 string // natural|vivid
	imageResponseFormat        string // url|b64_json
	imageTransparentBackground bool
	// Image prompt (optional). Not exposed via flags yet; populated when loading
	// from a saved messages file that contains an auxiliary "image_prompt" field.
	imagePrompt string
	// Message viewing modes
	prepDryRun    bool // When true, run pre-stage only, print refined messages to stdout, and exit
	printMessages bool // When true, pretty-print final merged messages to stderr before main call
	// Streaming control
	streamFinal bool // When true, request SSE streaming and print only assistant{channel:"final"} progressively
	// Save/load refined messages
	saveMessagesPath string // When set, write the final merged Harmony messages to this JSON path and continue
	loadMessagesPath string // When set, bypass pre-stage and prompt; load messages JSON verbatim (validator-checked)
	// Custom channel routing: map specific assistant channels to stdout|stderr|omit
	channelRoutes map[string]string
	// Raw repeatable flag values for -channel-route parsing (e.g., "critic=stdout")
	channelRoutePairs []string
	// parseError carries a human-readable parse error for early exit situations
	parseError string
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

// boolFlexFlag wires a bool destination and records if it was set via flag.
type boolFlexFlag struct {
	dst *bool
	set *bool
}

func (b *boolFlexFlag) String() string {
	if b == nil || b.dst == nil {
		return "false"
	}
	if *b.dst {
		return "true"
	}
	return "false"
}

func (b *boolFlexFlag) Set(s string) error {
	v, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return err
	}
	if b.dst != nil {
		*b.dst = v
	}
	if b.set != nil {
		*b.set = true
	}
	return nil
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

// intFlexFlag wires an int destination and records if it was set via flag.
type intFlexFlag struct {
	dst *int
	set *bool
}

func (f *intFlexFlag) String() string {
	if f == nil || f.dst == nil {
		return "0"
	}
	return strconv.Itoa(*f.dst)
}

func (f *intFlexFlag) Set(s string) error {
	v, err := strconv.Atoi(strings.TrimSpace(s))
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

// parseFlags parses command-line flags and environment variables.
// nolint:gocyclo // Flag definition and precedence resolution are inherently branching but covered by tests.
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
	// Role input flags
	// -developer is repeatable; collect via custom sliceVar
	flag.Var((*stringSliceFlag)(&cfg.developerPrompts), "developer", "Developer message (repeatable)")
	flag.Var((*stringSliceFlag)(&cfg.developerFiles), "developer-file", "Path to file containing developer message (repeatable; '-' for STDIN)")
	flag.StringVar(&cfg.systemFile, "system-file", "", "Path to file containing system prompt ('-' for STDIN; mutually exclusive with -system)")
	flag.StringVar(&cfg.promptFile, "prompt-file", "", "Path to file containing user prompt ('-' for STDIN; mutually exclusive with -prompt)")
	// Pre-stage system message (optional). Precedence: flag > env > empty. Mutually exclusive with -prep-system-file
	flag.StringVar(&cfg.prepSystem, "prep-system", "", "Pre-stage system message (env OAI_PREP_SYSTEM; mutually exclusive with -prep-system-file)")
	flag.StringVar(&cfg.prepSystemFile, "prep-system-file", "", "Path to file containing pre-stage system message ('-' for STDIN; env OAI_PREP_SYSTEM_FILE; mutually exclusive with -prep-system)")
	flag.StringVar(&cfg.toolsPath, "tools", "", "Path to tools.json (optional)")
	// State directory (CLI > env > empty). When set, create if missing with 0700.
	flag.StringVar(&cfg.stateDir, "state-dir", getEnv("AGENTCLI_STATE_DIR", ""), "Directory to persist and restore execution state across runs (env AGENTCLI_STATE_DIR)")
	// Optional state scope (CLI > env > computed default)
	flag.StringVar(&cfg.stateScope, "state-scope", getEnv("AGENTCLI_STATE_SCOPE", ""), "Optional scope key to partition saved state (env AGENTCLI_STATE_SCOPE); when empty, a default hash of model|base_url|toolset is used")
	// Refinement flags
	flag.BoolVar(&cfg.stateRefine, "state-refine", false, "Refine the loaded state bundle using -state-refine-text or -state-refine-file (requires -state-dir)")
	flag.StringVar(&cfg.stateRefineText, "state-refine-text", "", "Refinement input text to apply to the loaded state bundle (ignored when -state-refine-file is set; requires -state-dir)")
	flag.StringVar(&cfg.stateRefineFile, "state-refine-file", "", "Path to file containing refinement input (wins over -state-refine-text; requires -state-dir)")
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
	flag.Float64Var(&cfg.prepTopP, "prep-top-p", 0, "Nucleus sampling probability mass for pre-stage (env OAI_PREP_TOP_P; conflicts with -prep-temp)")
	// Pre-stage explicit temperature override (flag > env OAI_PREP_TEMP > inherit -temp)
	var prepTempSet bool
	(func() {
		cfg.prepTemperature = -1 // sentinel to detect unset
		f := &float64FlexFlag{dst: &cfg.prepTemperature, set: &prepTempSet}
		flag.CommandLine.Var(f, "prep-temp", "Pre-stage sampling temperature (env OAI_PREP_TEMP; inherits -temp if unset; conflicts with -prep-top-p)")
	})()
	// Pre-stage profile selector (deterministic|general|creative|reasoning)
	var prepProfileRaw string
	flag.StringVar(&prepProfileRaw, "prep-profile", "", "Pre-stage prompt profile (deterministic|general|creative|reasoning); sets temperature when supported (conflicts with -prep-top-p)")
	// Pre-stage explicit overrides
	flag.StringVar(&cfg.prepModel, "prep-model", "", "Pre-stage model ID (env OAI_PREP_MODEL; inherits -model if unset)")
	flag.StringVar(&cfg.prepBaseURL, "prep-base-url", "", "Pre-stage base URL (env OAI_PREP_BASE_URL; inherits -base-url if unset)")
	flag.StringVar(&cfg.prepAPIKey, "prep-api-key", "", "Pre-stage API key (env OAI_PREP_API_KEY; falls back to OAI_API_KEY/OPENAI_API_KEY; inherits -api-key if unset)")
	flag.IntVar(&cfg.prepHTTPRetries, "prep-http-retries", 0, "Pre-stage HTTP retries (env OAI_PREP_HTTP_RETRIES; inherits -http-retries if unset)")
	flag.DurationVar(&cfg.prepHTTPBackoff, "prep-http-retry-backoff", 0, "Pre-stage HTTP retry backoff (env OAI_PREP_HTTP_RETRY_BACKOFF; inherits -http-retry-backoff if unset)")
	// Global HTTP retry knobs: precedence flag > env > default
	var httpRetriesSet bool
	(func() {
		cfg.httpRetries = -1 // sentinel to detect unset
		f := &intFlexFlag{dst: &cfg.httpRetries, set: &httpRetriesSet}
		flag.CommandLine.Var(f, "http-retries", "Number of retries for transient HTTP failures (timeouts, 429, 5xx) (env OAI_HTTP_RETRIES; default 2)")
	})()
	var httpBackoffSet bool
	(func() {
		cfg.httpBackoff = 0 // resolved after parsing
		f := durationFlexFlag{dst: &cfg.httpBackoff, set: &httpBackoffSet}
		flag.CommandLine.Var(f, "http-retry-backoff", "Base backoff between HTTP retry attempts (exponential) (env OAI_HTTP_RETRY_BACKOFF; default 500ms)")
	})()
	flag.BoolVar(&cfg.debug, "debug", false, "Dump request/response JSON to stderr")
	flag.BoolVar(&cfg.verbose, "verbose", false, "Also print non-final assistant channels (critic/confidence) to stderr")
	flag.BoolVar(&cfg.quiet, "quiet", false, "Suppress non-final output; print only final text to stdout")
	flag.BoolVar(&cfg.prepToolsAllowExternal, "prep-tools-allow-external", false, "Allow pre-stage to execute external tools from -tools; when false, pre-stage is limited to built-in read-only tools")
	flag.StringVar(&cfg.prepToolsPath, "prep-tools", "", "Path to pre-stage tools.json (optional; used only with -prep-tools-allow-external)")
	flag.BoolVar(&cfg.prepCacheBust, "prep-cache-bust", false, "Skip pre-stage cache and force recompute")
	// Enabled by default; user can disable to skip pre-stage entirely. Track if explicitly set.
	cfg.prepEnabled = true
	flag.CommandLine.Var(&boolFlexFlag{dst: &cfg.prepEnabled, set: &cfg.prepEnabledSet}, "prep-enabled", "Enable pre-stage processing (default true; when false, skip pre-stage and proceed directly to main call)")
	// Message viewing flags
	flag.BoolVar(&cfg.prepDryRun, "prep-dry-run", false, "Run pre-stage only, print refined Harmony messages to stdout, and exit 0")
	flag.BoolVar(&cfg.printMessages, "print-messages", false, "Pretty-print the final merged message array to stderr before the main call")
	flag.BoolVar(&cfg.streamFinal, "stream-final", false, "If server supports streaming, stream only assistant{channel:\"final\"} to stdout; buffer other channels for -verbose")
	// Custom channel routing (repeatable): -channel-route name=stdout|stderr|omit
	flag.Var((*stringSliceFlag)(&cfg.channelRoutePairs), "channel-route", "Route assistant channels (final|critic|confidence) to stdout|stderr|omit; repeatable, e.g., -channel-route critic=stdout")
	// Save/load refined messages
	flag.StringVar(&cfg.saveMessagesPath, "save-messages", "", "Write the final merged Harmony messages to the given JSON file and continue")
	flag.StringVar(&cfg.loadMessagesPath, "load-messages", "", "Bypass pre-stage and prompt; load Harmony messages from the given JSON file (validator-checked)")
	flag.BoolVar(&cfg.capabilities, "capabilities", false, "Print enabled tools and exit")
	flag.BoolVar(&cfg.printConfig, "print-config", false, "Print resolved config and exit")
	// Global dry-run for state persistence planning (no disk writes)
	flag.BoolVar(&cfg.dryRun, "dry-run", false, "Print intended state actions (restore/refine/save) and exit without writing state")
	// Image API flags
	flag.StringVar(&cfg.imageBaseURL, "image-base-url", "", "Image API base URL (env OAI_IMAGE_BASE_URL; inherits -base-url if unset)")
	flag.StringVar(&cfg.imageAPIKey, "image-api-key", "", "Image API key (env OAI_IMAGE_API_KEY; inherits -api-key if unset; falls back to OPENAI_API_KEY)")
	// Image model flag (precedence: flag > env > default)
	defaultImageModel := getEnv("OAI_IMAGE_MODEL", "gpt-image-1")
	flag.StringVar(&cfg.imageModel, "image-model", defaultImageModel, "Image model ID (env OAI_IMAGE_MODEL; default gpt-image-1)")
	// Image HTTP behavior flags
	// Timeout (duration)
	flag.DurationVar(&cfg.imageHTTPTimeout, "image-http-timeout", 0, "Image HTTP timeout (env OAI_IMAGE_HTTP_TIMEOUT; inherits -http-timeout if unset)")
	flag.IntVar(&cfg.imageHTTPRetries, "image-http-retries", 0, "Image HTTP retries (env OAI_IMAGE_HTTP_RETRIES; inherits -http-retries if unset)")
	flag.DurationVar(&cfg.imageHTTPBackoff, "image-http-retry-backoff", 0, "Image HTTP base backoff (env OAI_IMAGE_HTTP_RETRY_BACKOFF; inherits -http-retry-backoff if unset)")
	flag.StringVar(&cfg.imageSize, "image-size", "1024x1024", "Image size (e.g., 256x256, 512x512, 1024x1024)")
	flag.StringVar(&cfg.imageQuality, "image-quality", "standard", "Image quality (standard|hd)")
	flag.StringVar(&cfg.imageStyle, "image-style", "natural", "Image style (natural|vivid)")
	flag.StringVar(&cfg.imageResponseFormat, "image-response-format", "url", "Image response format (url|b64_json)")
	flag.BoolVar(&cfg.imageTransparentBackground, "image-transparent-background", false, "Request transparent background for images when supported")
	// Image prompt (optional) is populated when loading messages from file

	// Deprecated: global timeout source tracking
	if httpSet {
		cfg.httpTimeoutSource = "flag"
	} else if os.Getenv("OAI_HTTP_TIMEOUT") != "" {
		cfg.httpTimeoutSource = "env"
	} else if globalSet {
		cfg.httpTimeoutSource = "global"
		if cfg.httpTimeout == 0 && cfg.timeout > 0 {
			cfg.httpTimeout = cfg.timeout
		}
	} else {
		cfg.httpTimeoutSource = "default"
	}
	if toolSet {
		cfg.toolTimeoutSource = "flag"
	} else if os.Getenv("OAI_TOOL_TIMEOUT") != "" {
		cfg.toolTimeoutSource = "env"
	} else if globalSet {
		cfg.toolTimeoutSource = "global"
		if cfg.toolTimeout == 0 && cfg.timeout > 0 {
			cfg.toolTimeout = cfg.timeout
		}
	} else {
		cfg.toolTimeoutSource = "default"
	}

	// Validate -system and -system-file not both set
	if strings.TrimSpace(cfg.systemPrompt) != "" && strings.TrimSpace(cfg.systemFile) != "" {
		cfg.parseError = "error: -system and -system-file are mutually exclusive"
		return cfg, 2
	}
	if strings.TrimSpace(cfg.prepSystem) != "" && strings.TrimSpace(cfg.prepSystemFile) != "" {
		cfg.parseError = "error: -prep-system and -prep-system-file are mutually exclusive"
		return cfg, 2
	}

	// Ensure -state-dir exists if provided
	if s := strings.TrimSpace(cfg.stateDir); s != "" {
		s = filepath.Clean(s)
		// Create directory tree with 0700, respecting umask
		if err := os.MkdirAll(s, 0o700); err != nil {
			cfg.parseError = fmt.Sprintf("error: creating -state-dir %q: %v", s, err)
			return cfg, 2
		}
		cfg.stateDir = s
	}
	// Resolve state scope: when empty, compute default from model|base_url|toolset_hash
	if strings.TrimSpace(cfg.stateScope) == "" {
		// Compute toolset hash from manifest if provided; empty string when no tools
		toolsetHash := computeToolsetHash(strings.TrimSpace(cfg.toolsPath))
		cfg.stateScope = computeDefaultStateScope(strings.TrimSpace(cfg.model), strings.TrimSpace(cfg.baseURL), toolsetHash)
	}
	// Validate refinement usage: any refine flags require -state-dir
	if cfg.stateRefine || strings.TrimSpace(cfg.stateRefineText) != "" || strings.TrimSpace(cfg.stateRefineFile) != "" {
		if strings.TrimSpace(cfg.stateDir) == "" {
			cfg.parseError = "error: state refinement (-state-refine, -state-refine-text, -state-refine-file) requires -state-dir to be set"
			return cfg, 2
		}
	}
	return cfg, 0
}
