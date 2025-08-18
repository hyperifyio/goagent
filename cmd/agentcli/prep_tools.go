package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/hyperifyio/goagent/internal/oai"
)

// appendPreStageBuiltinToolOutputs executes a restricted set of in-process, read-only
// tools for the pre-stage and appends their outputs (or deterministic error JSON)
// to the conversation messages. Supported tools:
//   - fs.read_file {path:string}
//   - fs.list_dir {path:string}
//   - fs.stat {path:string}
//   - env.get {key:string}
//   - os.info {}
//
// All paths must be repo-relative (no absolute paths, no parent traversal).
func appendPreStageBuiltinToolOutputs(messages []oai.Message, assistantMsg oai.Message, _ cliConfig) []oai.Message {
	for _, tc := range assistantMsg.ToolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		argsJSON := strings.TrimSpace(tc.Function.Arguments)
		if argsJSON == "" {
			argsJSON = "{}"
		}

		// Parse arguments into a generic map
		var args map[string]any
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			messages = append(messages, oai.Message{
				Role:       oai.RoleTool,
				Name:       name,
				ToolCallID: tc.ID,
				Content:    mustJSON(map[string]string{"error": "invalid arguments"}),
			})
			continue
		}

		// Dispatch by name
		switch name {
		case "fs.read_file":
			content, err := prepReadFile(args)
			if err != nil {
				messages = append(messages, oai.Message{Role: oai.RoleTool, Name: name, ToolCallID: tc.ID, Content: mustJSON(map[string]string{"error": err.Error()})})
			} else {
				messages = append(messages, oai.Message{Role: oai.RoleTool, Name: name, ToolCallID: tc.ID, Content: mustJSON(map[string]any{"content": content})})
			}
		case "fs.list_dir":
			entries, err := prepListDir(args)
			if err != nil {
				messages = append(messages, oai.Message{Role: oai.RoleTool, Name: name, ToolCallID: tc.ID, Content: mustJSON(map[string]string{"error": err.Error()})})
			} else {
				messages = append(messages, oai.Message{Role: oai.RoleTool, Name: name, ToolCallID: tc.ID, Content: mustJSON(map[string]any{"entries": entries})})
			}
		case "fs.stat":
			st, err := prepStat(args)
			if err != nil {
				messages = append(messages, oai.Message{Role: oai.RoleTool, Name: name, ToolCallID: tc.ID, Content: mustJSON(map[string]string{"error": err.Error()})})
			} else {
				messages = append(messages, oai.Message{Role: oai.RoleTool, Name: name, ToolCallID: tc.ID, Content: mustJSON(st)})
			}
		case "env.get":
			key := ""
			if kv, ok := args["key"].(string); ok {
				key = kv
			}
			val := os.Getenv(strings.TrimSpace(key))
			messages = append(messages, oai.Message{Role: oai.RoleTool, Name: name, ToolCallID: tc.ID, Content: mustJSON(map[string]string{"value": val})})
		case "os.info":
			messages = append(messages, oai.Message{Role: oai.RoleTool, Name: name, ToolCallID: tc.ID, Content: mustJSON(map[string]string{"goos": runtime.GOOS, "goarch": runtime.GOARCH})})
		default:
			// Unknown or disallowed tool names deterministically error
			messages = append(messages, oai.Message{Role: oai.RoleTool, Name: name, ToolCallID: tc.ID, Content: mustJSON(map[string]string{"error": fmt.Sprintf("unknown tool: %s", name)})})
		}
	}
	return messages
}

// mustJSON marshals v to a compact one-line JSON string. Falls back to a minimal error JSON.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{\"error\":\"internal error\"}"
	}
	// Collapse whitespace just in case
	s := string(b)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.Join(strings.Fields(s), " ")
}

func requireRepoRelativePath(args map[string]any) (string, error) {
	raw := ""
	if v, ok := args["path"].(string); ok {
		raw = v
	}
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("path is required")
	}
	// Reject absolute paths
	if filepath.IsAbs(raw) {
		return "", fmt.Errorf("path must be repo-relative")
	}
	// Clean and forbid parent traversal
	cleaned := filepath.Clean(strings.ReplaceAll(raw, "\\", "/"))
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("path must not contain parent traversal")
	}
	// Resolve against current working directory (acts as repo root in tests/CLI)
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	return abs, nil
}

func prepReadFile(args map[string]any) (string, error) {
	abs, err := requireRepoRelativePath(args)
	if err != nil {
		return "", err
	}
	// Read up to a reasonable size to avoid giant outputs; 256 KiB cap
	const capBytes = 256 * 1024
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	if len(data) > capBytes {
		data = data[:capBytes]
	}
	// Return as UTF-8 string; lossy but sufficient for read-only inspection
	return string(data), nil
}

type listEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // file|dir|other
}

func prepListDir(args map[string]any) ([]listEntry, error) {
	abs, err := requireRepoRelativePath(args)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	out := make([]listEntry, 0, len(entries))
	for _, e := range entries {
		typ := "file"
		if e.IsDir() {
			typ = "dir"
		}
		// Detect other types best-effort
		if !e.IsDir() {
			if info, ierr := e.Info(); ierr == nil {
				if (info.Mode() & fs.ModeSymlink) != 0 {
					typ = "other"
				}
			}
		}
		out = append(out, listEntry{Name: e.Name(), Type: typ})
	}
	// Deterministic order
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type statView struct {
	Size  int64 `json:"size"`
	IsDir bool  `json:"is_dir"`
}

func prepStat(args map[string]any) (statView, error) {
	abs, err := requireRepoRelativePath(args)
	if err != nil {
		return statView{}, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return statView{}, err
	}
	return statView{Size: fi.Size(), IsDir: fi.IsDir()}, nil
}
