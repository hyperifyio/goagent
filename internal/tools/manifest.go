package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/hyperifyio/goagent/internal/oai"
)

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"` // JSON Schema for params
	Command     []string        `json:"command"`          // argv: program and args
	TimeoutSec  int             `json:"timeoutSec,omitempty"`
}

type Manifest struct {
	Tools []ToolSpec `json:"tools"`
}

// LoadManifest reads tools.json and returns a name->spec registry and an OpenAI-compatible tools array.
// Relative command paths in the manifest are validated and then resolved relative to the manifest's directory,
// so they do not depend on the process working directory.
func LoadManifest(manifestPath string) (map[string]ToolSpec, []oai.Tool, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read manifest: %w", err)
	}
	var man Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return nil, nil, fmt.Errorf("parse manifest: %w", err)
	}
	registry := make(map[string]ToolSpec)
	var oaiTools []oai.Tool
	nameSeen := make(map[string]struct{})
	manifestDir := filepath.Dir(manifestPath)
	for i, t := range man.Tools {
		if t.Name == "" {
			return nil, nil, fmt.Errorf("tool[%d]: name is required", i)
		}
		if _, ok := nameSeen[t.Name]; ok {
			return nil, nil, fmt.Errorf("tool[%d] %q: duplicate name", i, t.Name)
		}
		nameSeen[t.Name] = struct{}{}
		if len(t.Command) < 1 {
			return nil, nil, fmt.Errorf("tool[%d] %q: command must have at least program name", i, t.Name)
		}
		// S52/S30: Harden command[0] validation. For any relative program path,
		// enforce the canonical tools bin prefix and prevent path escapes.
		cmd0 := t.Command[0]
        if !filepath.IsAbs(cmd0) {
            // Normalize separators: convert backslashes to slashes (works cross‑platform)
            // and then perform a platform-agnostic clean. Finally, ensure forward slashes.
            raw := strings.ReplaceAll(cmd0, "\\", "/")
            norm := filepath.ToSlash(path.Clean(raw))
			// Normalize to a consistent leading ./ for prefix checks
			if strings.HasPrefix(norm, "tools/") || norm == "tools" {
				norm = "./" + norm
			}
			// Reject leading parent traversal
			if strings.HasPrefix(norm, "../") || norm == ".." {
				return nil, nil, fmt.Errorf("tool[%d] %q: command[0] must not start with '..' or escape tools/bin (got %q)", i, t.Name, cmd0)
			}
			// If original referenced ./tools/bin, ensure cleaned still stays within ./tools/bin
			if strings.HasPrefix(raw, "./tools/bin/") || raw == "./tools/bin" {
				if !(strings.HasPrefix(norm, "./tools/bin/")) {
					return nil, nil, fmt.Errorf("tool[%d] %q: command[0] escapes ./tools/bin after normalization (got %q -> %q)", i, t.Name, cmd0, norm)
				}
			} else {
				// Enforce canonical prefix for all other relative commands
				if !strings.HasPrefix(norm, "./tools/bin/") {
					return nil, nil, fmt.Errorf("tool[%d] %q: relative command[0] must start with ./tools/bin/", i, t.Name)
				}
			}
			// Resolve relative program path against the manifest directory to avoid dependence on process CWD
			// Keep validation based on the normalized forward-slash path, but compute a concrete absolute filesystem path.
			// Example: manifest in /repo/sub/manifest/tools.json and command "./tools/bin/name" -> /repo/sub/manifest/tools/bin/name
			// Trim leading "./" for joining, then convert to OS-specific separators.
			trimmed := strings.TrimPrefix(norm, "./")
			resolved := filepath.Join(manifestDir, filepath.FromSlash(trimmed))
			absResolved, errAbs := filepath.Abs(resolved)
			if errAbs != nil {
				return nil, nil, fmt.Errorf("tool[%d] %q: resolve command[0]: %v", i, t.Name, errAbs)
			}
			t.Command[0] = absResolved
		}
		registry[t.Name] = t
		// Build OpenAI tools entry
		entry := oai.Tool{
			Type: "function",
			Function: oai.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		}
		oaiTools = append(oaiTools, entry)
	}
	return registry, oaiTools, nil
}
