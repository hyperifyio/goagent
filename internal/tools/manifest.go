package tools

import (
	"encoding/json"
	"fmt"
	"os"
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
        // S52: Harden command[0] validation to prevent path escapes when relative
        cmd0 := t.Command[0]
        if !filepath.IsAbs(cmd0) {
            raw := filepath.ToSlash(cmd0)
            norm := filepath.ToSlash(filepath.Clean(cmd0))
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
                if !(strings.HasPrefix(norm, "./tools/bin/") || norm == "./tools/bin") {
                    return nil, nil, fmt.Errorf("tool[%d] %q: command[0] escapes ./tools/bin after normalization (got %q -> %q)", i, t.Name, cmd0, norm)
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
