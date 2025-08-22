package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/hyperifyio/goagent/internal/oai"
	"github.com/hyperifyio/goagent/internal/oai/prestage"
	"github.com/hyperifyio/goagent/internal/tools"
)

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
            if parsed, pErr := prestage.ParsePrestagePayload(payload); pErr == nil {
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

// appendPreStageBuiltinToolOutputs executes built-in read-only pre-stage tools.
// For now this is a no-op placeholder to keep behavior deterministic without external tools.
func appendPreStageBuiltinToolOutputs(messages []oai.Message, assistantMsg oai.Message, cfg cliConfig) []oai.Message {
	if len(assistantMsg.ToolCalls) == 0 {
		return messages
	}
	// Execute a minimal, safe subset inline without spawning processes.
	// Supported names: fs.read_file, os.info. Others are ignored.
	for _, tc := range assistantMsg.ToolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		switch name {
		case "fs.read_file":
			// Parse {"path":"..."}
			var args struct {
				Path string `json:"path"`
			}
			// Best-effort JSON decode; ignore on error
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			var content string
			if s := strings.TrimSpace(args.Path); s != "" {
				if b, err := os.ReadFile(s); err == nil {
					content = string(b)
				}
			}
			// Build deterministic one-line JSON
			payload, _ := json.Marshal(map[string]any{
				"path":    strings.TrimSpace(args.Path),
				"content": content,
			})
			messages = append(messages, oai.Message{
				Role:       oai.RoleTool,
				ToolCallID: strings.TrimSpace(tc.ID),
				Name:       name,
				Content:    oneLine(string(payload)),
			})
		case "os.info":
			// Emit GOOS/GOARCH and process pid
			info := map[string]any{
				"goos":   runtime.GOOS,
				"goarch": runtime.GOARCH,
				"pid":    os.Getpid(),
			}
			payload, _ := json.Marshal(info)
			messages = append(messages, oai.Message{
				Role:       oai.RoleTool,
				ToolCallID: strings.TrimSpace(tc.ID),
				Name:       name,
				Content:    oneLine(string(payload)),
			})
		default:
			// Ignore unsupported built-ins for now
		}
	}
	return messages
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
