package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

    "github.com/hyperifyio/goagent/internal/oai"
)

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
