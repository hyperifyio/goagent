package main

import (
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
