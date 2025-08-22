package prestage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperifyio/goagent/internal/state"
)

// readLatestPointer is a tiny helper to read latest.json's path field.
func readLatestPointer(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "latest.json"))
	if err != nil {
		t.Fatalf("read latest.json: %v", err)
	}
	var ptr struct {
		Version string `json:"version"`
		Path    string `json:"path"`
		SHA256  string `json:"sha256"`
	}
	if err := json.Unmarshal(b, &ptr); err != nil {
		t.Fatalf("unmarshal latest.json: %v", err)
	}
	if ptr.Version != "1" || strings.TrimSpace(ptr.Path) == "" {
		t.Fatalf("invalid latest pointer: %#v", ptr)
	}
	return ptr.Path
}

func countSnapshotFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "state-") && strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

func TestE2E_State_SaveAndRestore(t *testing.T) {
	tmp := t.TempDir()
	scope := "testscope"
	b1 := &state.StateBundle{
		Version:     "1",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		ToolVersion: "test",
		ModelID:     "gpt-x",
		BaseURL:     "http://api.example",
		ToolsetHash: "toolset-1",
		ScopeKey:    scope,
		Prompts:     map[string]string{"system": "S", "developer": "dev1"},
	}
	b1.SourceHash = state.ComputeSourceHash(b1.ModelID, b1.BaseURL, b1.ToolsetHash, b1.ScopeKey)
	if err := state.SaveStateBundle(tmp, b1); err != nil {
		t.Fatalf("SaveStateBundle(b1): %v", err)
	}
	_ = readLatestPointer(t, tmp)
	if count := countSnapshotFiles(t, tmp); count != 1 {
		t.Fatalf("expected 1 snapshot, got %d", count)
	}
	s1 := &stubRunner{}
	c1 := &Coordinator{StateDir: tmp, ScopeKey: scope, Runner: s1}
	out1, err := c1.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute (restore) error: %v", err)
	}
	if !out1.UsedRestore || out1.Restored == nil {
		t.Fatalf("expected restore, got %+v", out1)
	}
	if s1.calls != 0 {
		t.Fatalf("runner should not be called on restore, calls=%d", s1.calls)
	}
}

func TestE2E_State_RefineAdvancesLatest(t *testing.T) {
	tmp := t.TempDir()
	scope := "testscope"
	b1 := &state.StateBundle{
		Version:     "1",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		ToolVersion: "test",
		ModelID:     "gpt-x",
		BaseURL:     "http://api.example",
		ToolsetHash: "toolset-1",
		ScopeKey:    scope,
		Prompts:     map[string]string{"system": "S", "developer": "dev1"},
	}
	b1.SourceHash = state.ComputeSourceHash(b1.ModelID, b1.BaseURL, b1.ToolsetHash, b1.ScopeKey)
	if err := state.SaveStateBundle(tmp, b1); err != nil {
		t.Fatalf("SaveStateBundle(b1): %v", err)
	}
	firstPtr := readLatestPointer(t, tmp)
	prev, err := state.LoadLatestStateBundle(tmp)
	if err != nil || prev == nil {
		t.Fatalf("LoadLatestStateBundle: %v, prev=%v", err, prev)
	}
	b2, err := state.RefineStateBundle(prev, "tighten temperature to 0.2", "hello user")
	if err != nil {
		t.Fatalf("RefineStateBundle: %v", err)
	}
	if err := state.SaveStateBundle(tmp, b2); err != nil {
		t.Fatalf("SaveStateBundle(b2): %v", err)
	}
	secondPtr := readLatestPointer(t, tmp)
	if secondPtr == firstPtr {
		t.Fatalf("latest pointer did not advance: %q", secondPtr)
	}
	if count := countSnapshotFiles(t, tmp); count != 2 {
		t.Fatalf("expected 2 snapshots after refine save, got %d", count)
	}
}

func TestE2E_State_PromptPrecedence(t *testing.T) {
	tmp := t.TempDir()
	scope := "testscope"
	b1 := &state.StateBundle{
		Version:     "1",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		ToolVersion: "test",
		ModelID:     "gpt-x",
		BaseURL:     "http://api.example",
		ToolsetHash: "toolset-1",
		ScopeKey:    scope,
		Prompts:     map[string]string{"system": "S", "developer": "dev1"},
	}
	b1.SourceHash = state.ComputeSourceHash(b1.ModelID, b1.BaseURL, b1.ToolsetHash, b1.ScopeKey)
	if err := state.SaveStateBundle(tmp, b1); err != nil {
		t.Fatalf("SaveStateBundle(b1): %v", err)
	}
	// Overrides win over restore
	s2 := &stubRunner{}
	c2 := &Coordinator{StateDir: tmp, ScopeKey: scope, PrepPrompts: []string{"OVERRIDE_PROMPT"}, Runner: s2}
	out2, err := c2.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute (override) error: %v", err)
	}
	if out2.UsedRestore {
		t.Fatalf("did not expect restore when overrides provided")
	}
	if s2.calls != 1 || s2.lastPrompt != "OVERRIDE_PROMPT" {
		t.Fatalf("runner should be called with override; calls=%d, prompt=%q", s2.calls, s2.lastPrompt)
	}
	// Refine, then restore refined without overrides
	prev, err := state.LoadLatestStateBundle(tmp)
	if err != nil || prev == nil {
		t.Fatalf("LoadLatestStateBundle: %v, prev=%v", err, prev)
	}
	b2, err := state.RefineStateBundle(prev, "tighten temperature to 0.2", "hello user")
	if err != nil {
		t.Fatalf("RefineStateBundle: %v", err)
	}
	if err := state.SaveStateBundle(tmp, b2); err != nil {
		t.Fatalf("SaveStateBundle(b2): %v", err)
	}
	s3 := &stubRunner{}
	c3 := &Coordinator{StateDir: tmp, ScopeKey: scope, Runner: s3}
	out3, err := c3.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute (restore refined) error: %v", err)
	}
	if !out3.UsedRestore || out3.Restored == nil {
		t.Fatalf("expected restore of refined bundle, got %+v", out3)
	}
	dev := out3.Restored.Prompts["developer"]
	if !strings.Contains(dev, "tighten temperature to 0.2") || !strings.Contains(dev, "USER: hello user") {
		t.Fatalf("refined developer prompt missing expected parts: %q", dev)
	}
}
