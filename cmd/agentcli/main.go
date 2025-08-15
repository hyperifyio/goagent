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
	timeout      time.Duration
	temperature  float64
	debug        bool
    capabilities bool
}

func getEnv(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func parseFlags() (cliConfig, int) {
	var cfg cliConfig

	defaultSystem := "You are a helpful, precise assistant. Use tools when strictly helpful."
	defaultBase := getEnv("OAI_BASE_URL", "https://api.openai.com/v1")
	defaultModel := getEnv("OAI_MODEL", "oss-gpt-20b")
	defaultKey := getEnv("OAI_API_KEY", "")

	flag.StringVar(&cfg.prompt, "prompt", "", "User prompt (required)")
	flag.StringVar(&cfg.toolsPath, "tools", "", "Path to tools.json (optional)")
	flag.StringVar(&cfg.systemPrompt, "system", defaultSystem, "System prompt")
	flag.StringVar(&cfg.baseURL, "base-url", defaultBase, "OpenAI-compatible base URL")
	flag.StringVar(&cfg.apiKey, "api-key", defaultKey, "API key (if required by the endpoint)")
	flag.StringVar(&cfg.model, "model", defaultModel, "Model ID")
	flag.IntVar(&cfg.maxSteps, "max-steps", 8, "Maximum reasoning/tool steps")
	flag.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "HTTP and per-tool timeout")
	flag.Float64Var(&cfg.temperature, "temp", 0.2, "Sampling temperature")
    flag.BoolVar(&cfg.debug, "debug", false, "Dump request/response JSON to stderr")
    flag.BoolVar(&cfg.capabilities, "capabilities", false, "Print enabled tools and exit")
	flag.Parse()

    if !cfg.capabilities && strings.TrimSpace(cfg.prompt) == "" {
		return cfg, 2 // CLI misuse
	}
	return cfg, 0
}

func main() {
	cfg, exitOn := parseFlags()
	if exitOn != 0 {
		fmt.Fprintln(os.Stderr, "error: -prompt is required")
		os.Exit(exitOn)
	}
    if cfg.capabilities {
        code := printCapabilities(cfg, os.Stdout, os.Stderr)
        os.Exit(code)
    }
	code := runAgent(cfg, os.Stdout, os.Stderr)
	os.Exit(code)
}

// runAgent executes the non-interactive agent loop and returns a process exit code.
func runAgent(cfg cliConfig, stdout io.Writer, stderr io.Writer) int {
	// Load tools manifest if provided
	var (
		toolRegistry map[string]tools.ToolSpec
		oaiTools     []oai.Tool
	)
	var err error
	if strings.TrimSpace(cfg.toolsPath) != "" {
		toolRegistry, oaiTools, err = tools.LoadManifest(cfg.toolsPath)
		if err != nil {
			fmt.Fprintf(stderr, "error: failed to load tools manifest: %v\n", err)
			return 1
		}
		// Validate each configured tool is available on this system before proceeding
		for name, spec := range toolRegistry {
			if len(spec.Command) == 0 {
				fmt.Fprintf(stderr, "error: configured tool %q has no command\n", name)
				return 1
			}
			if _, lookErr := exec.LookPath(spec.Command[0]); lookErr != nil {
				fmt.Fprintf(stderr, "error: configured tool %q is unavailable: %v (program %q)\n", name, lookErr, spec.Command[0])
				return 1
			}
		}
	}

	httpClient := oai.NewClient(cfg.baseURL, cfg.apiKey, cfg.timeout)

	messages := []oai.Message{
		{Role: oai.RoleSystem, Content: cfg.systemPrompt},
		{Role: oai.RoleUser, Content: cfg.prompt},
	}

	// Loop with per-request timeouts so multi-step tool calls have full budget each time.
	for step := 0; step < cfg.maxSteps; step++ {
		req := oai.ChatCompletionsRequest{
			Model:       cfg.model,
			Messages:    messages,
			Temperature: &cfg.temperature,
		}
		if len(oaiTools) > 0 {
			req.Tools = oaiTools
			req.ToolChoice = "auto"
		}

		if cfg.debug {
			dump, _ := json.MarshalIndent(req, "", "  ")
			fmt.Fprintf(stderr, "\n--- chat.request step=%d ---\n%s\n", step+1, string(dump))
		}

		// Per-call context
		callCtx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
		resp, err := httpClient.CreateChatCompletion(callCtx, req)
		cancel()
		if err != nil {
			fmt.Fprintf(stderr, "error: chat call failed: %v\n", err)
			return 1
		}
		if cfg.debug {
			dump, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Fprintf(stderr, "\n--- chat.response step=%d ---\n%s\n", step+1, string(dump))
		}

		if len(resp.Choices) == 0 {
			fmt.Fprintln(stderr, "error: chat response has no choices")
			return 1
		}

		choice := resp.Choices[0]
		msg := choice.Message

		// If the model returned tool calls and we have a registry, execute them sequentially.
		if len(msg.ToolCalls) > 0 && len(toolRegistry) > 0 {
			for _, tc := range msg.ToolCalls {
				spec, ok := toolRegistry[tc.Function.Name]
				if !ok {
					// Append an error tool result and continue; do not exit.
					toolErr := map[string]string{"error": fmt.Sprintf("unknown tool: %s", tc.Function.Name)}
					contentBytes, _ := json.Marshal(toolErr)
					messages = append(messages, oai.Message{
						Role:       oai.RoleTool,
						Name:       tc.Function.Name,
						ToolCallID: tc.ID,
						Content:    string(contentBytes),
					})
					continue
				}

				// Prepare stdin as the raw JSON args text from the model
				argsJSON := strings.TrimSpace(tc.Function.Arguments)
				// Guard against empty string; always provide at least {}
				if argsJSON == "" {
					argsJSON = "{}"
				}

				// Per-tool timeout is handled inside RunToolWithJSON; pass a background parent.
				out, runErr := tools.RunToolWithJSON(context.Background(), spec, []byte(argsJSON), cfg.timeout)
				content := sanitizeToolContent(out, runErr)
				messages = append(messages, oai.Message{
					Role:       oai.RoleTool,
					Name:       tc.Function.Name,
					ToolCallID: tc.ID,
					Content:    content,
				})
			}
			// Continue loop for another assistant response using appended tool outputs
			continue
		}

		// If the model returned final assistant content, print and exit 0
		if msg.Role == oai.RoleAssistant && strings.TrimSpace(msg.Content) != "" {
			fmt.Fprintln(stdout, strings.TrimSpace(msg.Content))
			return 0
		}

		// Otherwise, append message and continue (some models return assistant with empty content and no tools)
		messages = append(messages, msg)
	}

	fmt.Fprintln(stderr, "error: run ended without final assistant content")
	return 1
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
	b, _ := json.Marshal(map[string]string{"error": msg})
	return oneLine(string(b))
}

func oneLine(s string) string {
	// Collapse newlines and tabs
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	// Collapse repeated spaces
	return strings.Join(strings.Fields(s), " ")
}

// printCapabilities loads the tools manifest (if provided) and prints a concise list
// of enabled tools along with a prominent safety warning. Returns a process exit code.
func printCapabilities(cfg cliConfig, stdout io.Writer, stderr io.Writer) int {
    // If no tools path provided, report no tools and exit 0
    if strings.TrimSpace(cfg.toolsPath) == "" {
        fmt.Fprintln(stdout, "No tools enabled (run with -tools <path to tools.json>).")
        fmt.Fprintln(stdout, "WARNING: Enabling tools allows local process execution and may permit network access. Review tools.json carefully.")
        return 0
    }

    registry, _, err := tools.LoadManifest(cfg.toolsPath)
    if err != nil {
        fmt.Fprintf(stderr, "error: failed to load tools manifest: %v\n", err)
        return 1
    }
    fmt.Fprintln(stdout, "WARNING: Enabled tools can execute local binaries and may access the network. Use with caution.")
    if len(registry) == 0 {
        fmt.Fprintln(stdout, "No tools enabled in manifest.")
        return 0
    }
    fmt.Fprintln(stdout, "Capabilities (enabled tools):")
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
        fmt.Fprintf(stdout, "- %s: %s\n", name, desc)
    }
    return 0
}
