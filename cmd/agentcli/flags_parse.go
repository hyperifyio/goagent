package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hyperifyio/goagent/internal/oai"
)

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
	var imageHTTPTimeoutSet bool
	cfg.imageHTTPTimeout = 0
	flag.Var(durationFlexFlag{dst: &cfg.imageHTTPTimeout, set: &imageHTTPTimeoutSet}, "image-http-timeout", "Image HTTP timeout (env OAI_IMAGE_HTTP_TIMEOUT; inherits -http-timeout if unset)")
	// Retries (int)
	var imageHTTPRetriesSet bool
	cfg.imageHTTPRetries = -1 // sentinel for unset
	flag.Var(&intFlexFlag{dst: &cfg.imageHTTPRetries, set: &imageHTTPRetriesSet}, "image-http-retries", "Image HTTP retries (env OAI_IMAGE_HTTP_RETRIES; inherits -http-retries if unset)")
	// Backoff (duration)
	var imageHTTPBackoffSet bool
	cfg.imageHTTPBackoff = 0
	flag.Var(durationFlexFlag{dst: &cfg.imageHTTPBackoff, set: &imageHTTPBackoffSet}, "image-http-retry-backoff", "Image HTTP retry backoff (env OAI_IMAGE_HTTP_RETRY_BACKOFF; inherits -http-retry-backoff if unset)")
	// Image parameter pass-through flags (precedence: flag > env > default)
	// -image-n
	cfg.imageN = -1 // sentinel for unset
	var imageNSet bool
	flag.Var(&intFlexFlag{dst: &cfg.imageN, set: &imageNSet}, "image-n", "Number of images to generate (env OAI_IMAGE_N; default 1)")
	// -image-size
	flag.StringVar(&cfg.imageSize, "image-size", "", "Image size WxH, e.g., 1024x1024 (env OAI_IMAGE_SIZE; default 1024x1024)")
	// -image-quality
	flag.StringVar(&cfg.imageQuality, "image-quality", "", "Image quality: standard|hd (env OAI_IMAGE_QUALITY; default standard)")
	// -image-style
	flag.StringVar(&cfg.imageStyle, "image-style", "", "Image style: natural|vivid (env OAI_IMAGE_STYLE; default natural)")
	// -image-response-format
	flag.StringVar(&cfg.imageResponseFormat, "image-response-format", "", "Image response format: url|b64_json (env OAI_IMAGE_RESPONSE_FORMAT; default url)")
	// -image-transparent-background
	flag.CommandLine.Var(&boolFlexFlag{dst: &cfg.imageTransparentBackground}, "image-transparent-background", "Request transparent background when supported (env OAI_IMAGE_TRANSPARENT_BACKGROUND; default false)")
	ignoreError(flag.CommandLine.Parse(os.Args[1:]))
	if strings.TrimSpace(prepProfileRaw) != "" {
		cfg.prepProfile = oai.PromptProfile(strings.TrimSpace(prepProfileRaw))
	}

	// Apply env precedence for pre-stage system fields when flags unset
	if strings.TrimSpace(cfg.prepSystem) == "" {
		if v := strings.TrimSpace(os.Getenv("OAI_PREP_SYSTEM")); v != "" {
			cfg.prepSystem = v
		}
	}
	if strings.TrimSpace(cfg.prepSystemFile) == "" {
		if v := strings.TrimSpace(os.Getenv("OAI_PREP_SYSTEM_FILE")); v != "" {
			cfg.prepSystemFile = v
		}
	}

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

	// Resolve pre-stage top_p from env when not set via flag
	var prepTopPFromEnv bool
	if cfg.prepTopP <= 0 {
		if v := strings.TrimSpace(os.Getenv("OAI_PREP_TOP_P")); v != "" {
			if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed > 0 {
				cfg.prepTopP = parsed
				prepTopPFromEnv = true
			}
		}
	}

	// Resolve pre-stage temperature precedence: flag > env > inherit from -temp
	if prepTempSet {
		cfg.prepTemperatureSource = "flag"
	} else if v := strings.TrimSpace(os.Getenv("OAI_PREP_TEMP")); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.prepTemperature = parsed
			cfg.prepTemperatureSource = "env"
		}
	}
	if cfg.prepTemperature < 0 { // still unset
		cfg.prepTemperature = cfg.temperature
		cfg.prepTemperatureSource = "inherit"
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

	// Resolve global HTTP retry knobs using centralized helpers
	// http-retries: flag > env > default(2)
	{
		resolved, _ := oai.ResolveInt(httpRetriesSet, cfg.httpRetries, os.Getenv("OAI_HTTP_RETRIES"), nil, 2)
		cfg.httpRetries = resolved
	}
	// http-retry-backoff: flag > env > default(500ms)
	{
		resolved, _ := oai.ResolveDuration(httpBackoffSet, cfg.httpBackoff, os.Getenv("OAI_HTTP_RETRY_BACKOFF"), nil, 500*time.Millisecond)
		cfg.httpBackoff = resolved
	}

	// Resolve prep overrides precedence: flag > env OAI_PREP_* > inherit main-call
	// Model
	if strings.TrimSpace(cfg.prepModel) != "" {
		cfg.prepModelSource = "flag"
	} else if v := strings.TrimSpace(os.Getenv("OAI_PREP_MODEL")); v != "" {
		cfg.prepModel = v
		cfg.prepModelSource = "env"
	} else {
		cfg.prepModel = cfg.model
		cfg.prepModelSource = "inherit"
	}
	// Base URL
	if strings.TrimSpace(cfg.prepBaseURL) != "" {
		cfg.prepBaseURLSource = "flag"
	} else if v := strings.TrimSpace(os.Getenv("OAI_PREP_BASE_URL")); v != "" {
		cfg.prepBaseURL = v
		cfg.prepBaseURLSource = "env"
	} else {
		cfg.prepBaseURL = cfg.baseURL
		cfg.prepBaseURLSource = "inherit"
	}
	// API key
	if strings.TrimSpace(cfg.prepAPIKey) != "" {
		cfg.prepAPIKeySource = "flag"
	} else if v := strings.TrimSpace(os.Getenv("OAI_PREP_API_KEY")); v != "" {
		cfg.prepAPIKey = v
		cfg.prepAPIKeySource = "env:OAI_PREP_API_KEY"
	} else if v := strings.TrimSpace(os.Getenv("OAI_API_KEY")); v != "" {
		cfg.prepAPIKey = v
		cfg.prepAPIKeySource = "env:OAI_API_KEY"
	} else if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); v != "" {
		cfg.prepAPIKey = v
		cfg.prepAPIKeySource = "env:OPENAI_API_KEY"
	} else {
		cfg.prepAPIKey = cfg.apiKey
		if strings.TrimSpace(cfg.apiKey) != "" {
			cfg.prepAPIKeySource = "inherit"
		} else {
			cfg.prepAPIKeySource = "empty"
		}
	}
	// HTTP retries
	if cfg.prepHTTPRetries > 0 {
		cfg.prepHTTPRetriesSource = "flag"
	} else if v := strings.TrimSpace(os.Getenv("OAI_PREP_HTTP_RETRIES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.prepHTTPRetries = n
			cfg.prepHTTPRetriesSource = "env"
		}
	}
	if cfg.prepHTTPRetries == 0 {
		cfg.prepHTTPRetries = cfg.httpRetries
		if cfg.prepHTTPRetriesSource == "" {
			cfg.prepHTTPRetriesSource = "inherit"
		}
	}
	// HTTP retry backoff
	if cfg.prepHTTPBackoff > 0 {
		cfg.prepHTTPBackoffSource = "flag"
	} else if v := strings.TrimSpace(os.Getenv("OAI_PREP_HTTP_RETRY_BACKOFF")); v != "" {
		if d, err := parseDurationFlexible(v); err == nil && d > 0 {
			cfg.prepHTTPBackoff = d
			cfg.prepHTTPBackoffSource = "env"
		}
	}
	if cfg.prepHTTPBackoff == 0 {
		cfg.prepHTTPBackoff = cfg.httpBackoff
		if cfg.prepHTTPBackoffSource == "" {
			cfg.prepHTTPBackoffSource = "inherit"
		}
	}

	// Resolve image config using helper (flag > env > inherit > fallback)
	if img, baseSrc, keySrc := oai.ResolveImageConfig(cfg.imageBaseURL, cfg.imageAPIKey, cfg.baseURL, cfg.apiKey); true {
		cfg.imageBaseURL = img.BaseURL
		cfg.imageAPIKey = img.APIKey
		cfg.imageBaseURLSource = baseSrc
		cfg.imageAPIKeySource = keySrc
	}

	// Resolve image HTTP knobs using centralized helpers with inheritance from main HTTP knobs
	// Timeout: flag > env > inherit(http-timeout) > default(unused)
	{
		inherit := cfg.httpTimeout
		resolved, src := oai.ResolveDuration(imageHTTPTimeoutSet, cfg.imageHTTPTimeout, os.Getenv("OAI_IMAGE_HTTP_TIMEOUT"), &inherit, cfg.httpTimeout)
		cfg.imageHTTPTimeout = resolved
		cfg.imageHTTPTimeoutSource = src
		if cfg.imageHTTPTimeout <= 0 && src == "inherit" {
			// Ensure a positive inherited timeout
			cfg.imageHTTPTimeout = cfg.httpTimeout
		}
	}
	// Retries: flag > env > inherit(http-retries) > default(unused)
	{
		inherit := cfg.httpRetries
		resolved, src := oai.ResolveInt(imageHTTPRetriesSet, cfg.imageHTTPRetries, os.Getenv("OAI_IMAGE_HTTP_RETRIES"), &inherit, cfg.httpRetries)
		cfg.imageHTTPRetries = resolved
		cfg.imageHTTPRetriesSource = src
		if cfg.imageHTTPRetries < 0 && src == "inherit" {
			cfg.imageHTTPRetries = cfg.httpRetries
		}
	}
	// Backoff: flag > env > inherit(http-retry-backoff) > default(unused)
	{
		inherit := cfg.httpBackoff
		resolved, src := oai.ResolveDuration(imageHTTPBackoffSet, cfg.imageHTTPBackoff, os.Getenv("OAI_IMAGE_HTTP_RETRY_BACKOFF"), &inherit, cfg.httpBackoff)
		cfg.imageHTTPBackoff = resolved
		cfg.imageHTTPBackoffSource = src
		if cfg.imageHTTPBackoff == 0 && src == "inherit" {
			cfg.imageHTTPBackoff = cfg.httpBackoff
		}
	}

	// Resolve image parameter pass-throughs with precedence: flag > env > default
	if !imageNSet {
		if v := strings.TrimSpace(os.Getenv("OAI_IMAGE_N")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 1 {
				cfg.imageN = n
			}
		}
	}
	if cfg.imageN < 0 {
		cfg.imageN = 1
	}
	if strings.TrimSpace(cfg.imageSize) == "" {
		if v := strings.TrimSpace(os.Getenv("OAI_IMAGE_SIZE")); v != "" {
			cfg.imageSize = v
		}
	}
	if strings.TrimSpace(cfg.imageSize) == "" {
		cfg.imageSize = "1024x1024"
	}
	if strings.TrimSpace(cfg.imageQuality) == "" {
		if v := strings.TrimSpace(os.Getenv("OAI_IMAGE_QUALITY")); v != "" {
			cfg.imageQuality = v
		}
	}
	if strings.TrimSpace(cfg.imageQuality) == "" {
		cfg.imageQuality = "standard"
	}
	if strings.TrimSpace(cfg.imageStyle) == "" {
		if v := strings.TrimSpace(os.Getenv("OAI_IMAGE_STYLE")); v != "" {
			cfg.imageStyle = v
		}
	}
	if strings.TrimSpace(cfg.imageStyle) == "" {
		cfg.imageStyle = "natural"
	}
	if strings.TrimSpace(cfg.imageResponseFormat) == "" {
		if v := strings.TrimSpace(os.Getenv("OAI_IMAGE_RESPONSE_FORMAT")); v != "" {
			cfg.imageResponseFormat = v
		}
	}
	if strings.TrimSpace(cfg.imageResponseFormat) == "" {
		cfg.imageResponseFormat = "url"
	}
	// Transparent background flag from env if flag not explicitly set
	if !cfg.imageTransparentBackground {
		if v := strings.TrimSpace(os.Getenv("OAI_IMAGE_TRANSPARENT_BACKGROUND")); v != "" {
			if b, err := strconv.ParseBool(v); err == nil {
				cfg.imageTransparentBackground = b
			}
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

	// Enforce mutual exclusion and required prompt presence (unless print-only modes)
	if strings.TrimSpace(cfg.systemFile) != "" && strings.TrimSpace(cfg.systemPrompt) != "" && cfg.systemPrompt != defaultSystem {
		// Both -system and -system-file provided (with -system not defaulted)
		cfg.parseError = "error: -system and -system-file are mutually exclusive"
		return cfg, 2
	}
	// Mutual exclusion for pre-stage system inputs
	if strings.TrimSpace(cfg.prepSystem) != "" && strings.TrimSpace(cfg.prepSystemFile) != "" {
		cfg.parseError = "error: -prep-system and -prep-system-file are mutually exclusive"
		return cfg, 2
	}
	if strings.TrimSpace(cfg.promptFile) != "" && strings.TrimSpace(cfg.prompt) != "" {
		cfg.parseError = "error: -prompt and -prompt-file are mutually exclusive"
		return cfg, 2
	}
	if !cfg.capabilities && !cfg.printConfig {
		// Resolve effective prompt presence considering -prompt-file
		if strings.TrimSpace(cfg.loadMessagesPath) == "" && strings.TrimSpace(cfg.prompt) == "" && strings.TrimSpace(cfg.promptFile) == "" {
			return cfg, 2
		}
	}
	// Parse channel-route pairs and validate
	if len(cfg.channelRoutePairs) > 0 {
		cfg.channelRoutes = make(map[string]string)
		for _, pair := range cfg.channelRoutePairs {
			p := strings.TrimSpace(pair)
			if p == "" {
				continue
			}
			eq := strings.IndexByte(p, '=')
			if eq <= 0 || eq >= len(p)-1 {
				cfg.parseError = "error: invalid -channel-route value (expected name=stdout|stderr|omit)"
				return cfg, 2
			}
			name := strings.TrimSpace(p[:eq])
			dest := strings.TrimSpace(p[eq+1:])
			switch name {
			case "final", "critic", "confidence":
				// ok
			default:
				cfg.parseError = fmt.Sprintf("error: invalid -channel-route channel %q (allowed: final, critic, confidence)", name)
				return cfg, 2
			}
			switch dest {
			case "stdout", "stderr", "omit":
				// ok
			default:
				cfg.parseError = fmt.Sprintf("error: invalid -channel-route destination %q (allowed: stdout, stderr, omit)", dest)
				return cfg, 2
			}
			cfg.channelRoutes[name] = dest
		}
	}

	// Conflict checks for save/load flags
	if strings.TrimSpace(cfg.saveMessagesPath) != "" && strings.TrimSpace(cfg.loadMessagesPath) != "" {
		cfg.parseError = "error: -save-messages and -load-messages are mutually exclusive"
		return cfg, 2
	}
	if strings.TrimSpace(cfg.loadMessagesPath) != "" {
		// Loading messages conflicts with providing -prompt or -prompt-file
		if strings.TrimSpace(cfg.prompt) != "" || strings.TrimSpace(cfg.promptFile) != "" {
			cfg.parseError = "error: -load-messages cannot be combined with -prompt or -prompt-file"
			return cfg, 2
		}
	}
	// Prep top_p source labeling for config dump
	if cfg.prepTopP > 0 {
		if prepTopPFromEnv {
			cfg.prepTopPSource = "env"
		} else {
			cfg.prepTopPSource = "flag"
		}
	} else {
		cfg.prepTopPSource = "inherit"
	}
	// Normalize/expand state-dir and create with 0700 if set
	if s := strings.TrimSpace(cfg.stateDir); s != "" {
		// Expand leading ~ to the user's home directory
		if strings.HasPrefix(s, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				s = filepath.Join(home, strings.TrimPrefix(s, "~"))
			}
		}
		// Clean path and ensure it's absolute or relative within cwd; no wildcards
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
