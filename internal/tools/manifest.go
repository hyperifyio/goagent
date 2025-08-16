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
			// Normalize separators first (convert backslashes to slashes cross‑platform),
			// then apply a platform-agnostic clean.
			raw := strings.ReplaceAll(cmd0, "\\", "/")
			norm := path.Clean(raw)
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
