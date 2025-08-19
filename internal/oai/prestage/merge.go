package prestage

import (
	"encoding/json"
	"strings"

	"github.com/hyperifyio/goagent/internal/oai"
)

// ToolConfig captures optional tool enable/disable hints and arbitrary key hints
// produced by the pre-stage processor.
type ToolConfig struct {
	EnableTools []string       `json:"enable_tools"`
	Hints       map[string]any `json:"hints"`
}

// PrestageParsed represents structured data extracted from the pre-stage
// Harmony payload. Fields are optional; empty values indicate absence.
type PrestageParsed struct {
	System            string                 // optional replacement for the system prompt
	Developers        []string               // zero-or-more developer prompts to append
	ToolConfig        *ToolConfig            // optional tool configuration hints
	ImageInstructions map[string]any         // optional defaults for downstream image tools
}

// ParsePrestagePayload parses a JSON payload returned by the pre-stage model.
// The expected format is a JSON array where elements are either Harmony
// messages with {"role":"system|developer","content":"..."} or objects
// containing one of the keys {"system": string}, {"developer": string},
// {"tool_config": {enable_tools:[], hints:{}}}, or {"image_instructions": {...}}.
// Unknown objects are ignored to keep parsing forward-compatible.
func ParsePrestagePayload(payload string) (PrestageParsed, error) {
	var out PrestageParsed
	s := strings.TrimSpace(payload)
	if s == "" {
		return out, nil
	}
	// Accept either a raw array or a JSON value with surrounding whitespace
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		// If not an array of objects, attempt to parse as a single Harmony message object
		var single map[string]json.RawMessage
		if err2 := json.Unmarshal([]byte(s), &single); err2 != nil {
			return out, err
		}
		arr = []map[string]json.RawMessage{single}
	}

	for _, obj := range arr {
		// Prefer explicit Harmony role schema when present
		if rawRole, ok := obj["role"]; ok {
			var role string
			_ = json.Unmarshal(rawRole, &role)
			role = strings.ToLower(strings.TrimSpace(role))
			if role == oai.RoleSystem || role == oai.RoleDeveloper {
				var content string
				_ = json.Unmarshal(obj["content"], &content)
				content = strings.TrimSpace(content)
				if content == "" {
					continue
				}
				if role == oai.RoleSystem {
					// First system wins; later ones are ignored
					if out.System == "" {
						out.System = content
					}
				} else {
					out.Developers = append(out.Developers, content)
				}
				continue
			}
		}

		// Fallback: support key-based entries as used in the default prompt examples
		if rawSys, ok := obj["system"]; ok {
			var sys string
			if err := json.Unmarshal(rawSys, &sys); err == nil {
				sys = strings.TrimSpace(sys)
				if sys != "" && out.System == "" {
					out.System = sys
				}
			}
			continue
		}
		if rawDev, ok := obj["developer"]; ok {
			var dev string
			if err := json.Unmarshal(rawDev, &dev); err == nil {
				dev = strings.TrimSpace(dev)
				if dev != "" {
					out.Developers = append(out.Developers, dev)
				}
			}
			continue
		}
		if rawTool, ok := obj["tool_config"]; ok {
			var tc ToolConfig
			if err := json.Unmarshal(rawTool, &tc); err == nil {
				// Normalize empty slices/maps to nil to avoid noise
				if len(tc.EnableTools) == 0 {
					tc.EnableTools = nil
				}
				if len(tc.Hints) == 0 {
					tc.Hints = nil
				}
				// First tool_config wins; ignore subsequent ones
				if out.ToolConfig == nil {
					out.ToolConfig = &tc
				}
			}
			continue
		}
		if rawImg, ok := obj["image_instructions"]; ok {
			var ii map[string]any
			if err := json.Unmarshal(rawImg, &ii); err == nil {
				if len(ii) > 0 && out.ImageInstructions == nil {
					out.ImageInstructions = ii
				}
			}
			continue
		}
	}

	return out, nil
}

// MergePrestageIntoMessages merges parsed pre-stage outputs into the provided
// seed Harmony messages. It applies the following deterministic rules:
//  1. If parsed.System is non-empty, replace the first system message content.
//  2. Append parsed.Developers immediately before the first user message; when
//     no user message exists, append them to the end. CLI-provided developer
//     messages in the seed remain first, preserving precedence.
// Messages with other roles are preserved in their original order.
func MergePrestageIntoMessages(seed []oai.Message, parsed PrestageParsed) []oai.Message {
	// Replace system content when provided
	out := make([]oai.Message, len(seed))
	copy(out, seed)
	if strings.TrimSpace(parsed.System) != "" {
		for i := range out {
			if out[i].Role == oai.RoleSystem {
				out[i].Content = parsed.System
				break
			}
		}
	}

	// Determine insertion index: immediately before first user message
	insertIdx := -1
	for i := range out {
		if out[i].Role == oai.RoleUser {
			insertIdx = i
			break
		}
	}

	if len(parsed.Developers) == 0 {
		return out
	}

	// Build developer messages to insert
	devMsgs := make([]oai.Message, 0, len(parsed.Developers))
	for _, d := range parsed.Developers {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		devMsgs = append(devMsgs, oai.Message{Role: oai.RoleDeveloper, Content: d})
	}
	if len(devMsgs) == 0 {
		return out
	}

	if insertIdx < 0 || insertIdx > len(out) {
		// No user message; append to end
		return append(out, devMsgs...)
	}

	// Insert before user
	merged := make([]oai.Message, 0, len(out)+len(devMsgs))
	merged = append(merged, out[:insertIdx]...)
	merged = append(merged, devMsgs...)
	merged = append(merged, out[insertIdx:]...)
	return merged
}
