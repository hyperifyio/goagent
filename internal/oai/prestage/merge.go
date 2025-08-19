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
	System            string         // optional replacement for the system prompt
	Developers        []string       // zero-or-more developer prompts to append
	ToolConfig        *ToolConfig    // optional tool configuration hints
	ImageInstructions map[string]any // optional defaults for downstream image tools
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
	arr, err := parseToObjectArray(s)
	if err != nil {
		return out, err
	}
	for _, obj := range arr {
		updateParsedFromObject(obj, &out)
	}
	return out, nil
}

// parseToObjectArray accepts either a JSON array of objects or a single object and returns a slice.
func parseToObjectArray(s string) ([]map[string]json.RawMessage, error) {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		return arr, nil
	}
	var single map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &single); err != nil {
		return nil, err
	}
	return []map[string]json.RawMessage{single}, nil
}

// updateParsedFromObject mutates out based on recognized fields in obj.
func updateParsedFromObject(obj map[string]json.RawMessage, out *PrestageParsed) {
	if tryRoleBased(obj, out) {
		return
	}
	_ = tryKeyBased(obj, out)
}

// tryRoleBased handles objects using the explicit Harmony role schema.
func tryRoleBased(obj map[string]json.RawMessage, out *PrestageParsed) bool {
	rawRole, ok := obj["role"]
	if !ok {
		return false
	}
	var role string
	if err := json.Unmarshal(rawRole, &role); err != nil {
		return false
	}
	role = strings.ToLower(strings.TrimSpace(role))
	if role != oai.RoleSystem && role != oai.RoleDeveloper {
		return false
	}
	var content string
	if rawContent, ok := obj["content"]; ok {
		if err := json.Unmarshal(rawContent, &content); err != nil {
			return true
		}
		content = strings.TrimSpace(content)
	}
	if content == "" {
		return true
	}
	if role == oai.RoleSystem {
		if out.System == "" {
			out.System = content
		}
	} else {
		out.Developers = append(out.Developers, content)
	}
	return true
}

// tryKeyBased supports legacy key-based entries.
func tryKeyBased(obj map[string]json.RawMessage, out *PrestageParsed) bool {
	if rawSys, ok := obj["system"]; ok {
		var sys string
		if err := json.Unmarshal(rawSys, &sys); err == nil {
			sys = strings.TrimSpace(sys)
			if sys != "" && out.System == "" {
				out.System = sys
			}
		}
		return true
	}
	if rawDev, ok := obj["developer"]; ok {
		var dev string
		if err := json.Unmarshal(rawDev, &dev); err == nil {
			dev = strings.TrimSpace(dev)
			if dev != "" {
				out.Developers = append(out.Developers, dev)
			}
		}
		return true
	}
	if rawTool, ok := obj["tool_config"]; ok {
		var tc ToolConfig
		if err := json.Unmarshal(rawTool, &tc); err == nil {
			if len(tc.EnableTools) == 0 {
				tc.EnableTools = nil
			}
			if len(tc.Hints) == 0 {
				tc.Hints = nil
			}
			if out.ToolConfig == nil {
				out.ToolConfig = &tc
			}
		}
		return true
	}
	if rawImg, ok := obj["image_instructions"]; ok {
		var ii map[string]any
		if err := json.Unmarshal(rawImg, &ii); err == nil {
			if len(ii) > 0 && out.ImageInstructions == nil {
				out.ImageInstructions = ii
			}
		}
		return true
	}
	return false
}

// MergePrestageIntoMessages merges parsed pre-stage outputs into the provided
// seed Harmony messages. It applies the following deterministic rules:
//  1. If parsed.System is non-empty, replace the first system message content.
//  2. Append parsed.Developers immediately before the first user message; when
//     no user message exists, append them to the end. CLI-provided developer
//     messages in the seed remain first, preserving precedence.
//
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
