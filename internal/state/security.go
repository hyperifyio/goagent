package state

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"runtime"
	"strings"
	"syscall"
)

// ensureSecureStateDir validates the state directory on Unix-like systems.
// It rejects world-writable or non-owned directories to avoid leaking secrets.
// On non-Unix platforms (e.g., Windows), the checks are skipped.
func ensureSecureStateDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("empty state dir")
	}
	// Only enforce on Unix-like systems. Windows ACLs differ and Mode().Perm() is not authoritative.
	if runtime.GOOS == "windows" {
		return nil
	}

	info, err := os.Stat(dir)
	if err != nil {
		// If it doesn't exist yet, the caller will create with 0700; allow.
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return errors.New("state dir is not a directory")
	}

	// Reject world-writable (others write bit set)
	if info.Mode().Perm()&0o002 != 0 {
		return errors.New("state dir is world-writable")
	}

	// Reject if not owned by current user (best-effort; skip if not supported)
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		uid := uint32(os.Getuid())
		if stat.Uid != uid {
			return errors.New("state dir is not owned by current user")
		}
	}
	return nil
}

// sanitizeBundleForSave returns a deep-copied and redacted bundle suitable for persistence.
// It avoids storing obvious secrets and raw bodies by:
//   - masking any values under keys containing "api_key", "apikey", "token", "authorization", "password", "secret"
//   - stripping Authorization header values (preserving scheme)
//   - removing long base64-like tokens (>=64 chars) within strings
//   - omitting likely raw request/response bodies under keys: request_body, response_body, raw_request, raw_response
func sanitizeBundleForSave(b *StateBundle) (*StateBundle, error) {
	if b == nil {
		return nil, errors.New("nil bundle")
	}

	// Shallow copy value fields and deep-copy maps
	out := &StateBundle{
		Version:     b.Version,
		CreatedAt:   b.CreatedAt,
		ToolVersion: b.ToolVersion,
		ModelID:     b.ModelID,
		BaseURL:     b.BaseURL,
		ToolsetHash: b.ToolsetHash,
		ScopeKey:    b.ScopeKey,
		SourceHash:  b.SourceHash,
		PrevSHA:     b.PrevSHA,
	}

	// Copy and sanitize string map
	if b.Prompts != nil {
		out.Prompts = make(map[string]string, len(b.Prompts))
		for k, v := range b.Prompts {
			out.Prompts[k] = sanitizeStringByHeuristics(k, v)
		}
	}
	// Copy and sanitize any maps
	out.PrepSettings = sanitizeAnyMap(b.PrepSettings)
	out.Context = sanitizeAnyMap(b.Context)
	out.ToolCaps = sanitizeAnyMap(b.ToolCaps)
	out.Custom = sanitizeAnyMap(b.Custom)

	// Validate round-trip JSON to ensure serializable
	if _, err := json.Marshal(out); err != nil {
		return nil, err
	}
	return out, nil
}

func sanitizeAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = sanitizeValue(k, v)
	}
	return out
}

func sanitizeAnySlice(key string, in []any) []any {
	if in == nil {
		return nil
	}
	out := make([]any, 0, len(in))
	for _, v := range in {
		out = append(out, sanitizeValue(key, v))
	}
	return out
}

var (
	// Detect long base64-like tokens (64+ chars of base64 charset)
	reLongB64 = regexp.MustCompile(`[A-Za-z0-9+/=]{64,}`)
	// Detect Authorization header anywhere in the string, case-insensitive
	reAuthAny = regexp.MustCompile(`(?i)authorization\s*:\s*(bearer|token|basic)\s+([A-Za-z0-9._\-]+)`) // keep scheme, mask token
)

func sanitizeValue(key string, v any) any {
	switch t := v.(type) {
	case string:
		lowerKey := strings.ToLower(key)
		// Omit likely raw bodies entirely
		if lowerKey == "request_body" || lowerKey == "response_body" || lowerKey == "raw_request" || lowerKey == "raw_response" {
			return "<omitted>"
		}
		return sanitizeStringByHeuristics(key, t)
	case map[string]any:
		return sanitizeAnyMap(t)
	case []any:
		return sanitizeAnySlice(key, t)
	default:
		// Leave numbers, bools, nil as-is
		return v
	}
}

func sanitizeStringByHeuristics(key string, s string) string {
	v := s
	lowerKey := strings.ToLower(key)
	// Redact inline Authorization headers regardless of key name
	v = reAuthAny.ReplaceAllStringFunc(v, func(match string) string {
		m := reAuthAny.FindStringSubmatch(match)
		if len(m) >= 3 {
			scheme := m[1]
			token := m[2]
			return "Authorization: " + scheme + " ****" + last4(token)
		}
		return "Authorization: <redacted>"
	})
	if containsAny(lowerKey, []string{"api_key", "apikey", "token", "password", "secret"}) {
		vv := strings.TrimSpace(v)
		if vv == "" {
			v = ""
		} else if len(vv) <= 4 {
			v = "****" + vv
		} else {
			v = "****" + vv[len(vv)-4:]
		}
	}
	// Redact long base64-like runs
	if reLongB64.MatchString(v) {
		v = reLongB64.ReplaceAllStringFunc(v, func(match string) string {
			if len(match) <= 4 {
				return "****" + match
			}
			return "****" + match[len(match)-4:]
		})
	}
	return v
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func last4(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}
