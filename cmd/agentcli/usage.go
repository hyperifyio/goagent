package main

import (
	"io"
	"strings"
)

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
