package prestage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperifyio/goagent/internal/oai"
	"github.com/hyperifyio/goagent/internal/state"
)

// stubRunner implements PrestageRunner for tests.
type stubRunner struct {
	calls      int
	lastPrompt string
	resp       oai.ChatCompletionsResponse
	err        error
}

func (s *stubRunner) Run(ctx context.Context, prompt string) (oai.ChatCompletionsResponse, error) {
	s.calls++
	s.lastPrompt = prompt
	return s.resp, s.err
}

func writeValidBundle(t *testing.T, dir string, scope string) *state.StateBundle {
	t.Helper()
	b := &state.StateBundle{
		Version:      "1",
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		ToolVersion:  "test",
		ModelID:      "gpt-x",
		BaseURL:      "http://example.test",
		ToolsetHash:  "abc",
		ScopeKey:     scope,
		Prompts:      map[string]string{"system": "S", "developer": "D"},
		PrepSettings: map[string]any{"k": "v"},
		Context:      map[string]any{"a": 1},
		ToolCaps:     map[string]any{"c": true},
		Custom:       map[string]any{"x": "y"},
		SourceHash:   state.ComputeSourceHash("gpt-x", "http://example.test", "abc", scope),
	}
	if err := state.SaveStateBundle(dir, b); err != nil {
		t.Fatalf("SaveStateBundle: %v", err)
	}
	return b
}

func TestCoordinator_UsesRestoreWhenAvailableAndNoOverrides(t *testing.T) {
	tmp := t.TempDir()
	bundle := writeValidBundle(t, tmp, "scope-1")

	c := &Coordinator{StateDir: tmp, ScopeKey: "scope-1", Runner: &stubRunner{}}
	out, err := c.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !out.UsedRestore || out.Restored == nil {
		t.Fatalf("expected restore to be used, got %+v", out)
	}
	if out.Restored.ScopeKey != bundle.ScopeKey {
		t.Fatalf("restored bundle mismatch: %+v", out.Restored)
	}
}

func TestCoordinator_SkipsRestoreOnOverridesAndCallsRunner(t *testing.T) {
	tmp := t.TempDir()
	_ = writeValidBundle(t, tmp, "scope-1")

	s := &stubRunner{resp: oai.ChatCompletionsResponse{Model: "m"}}
	c := &Coordinator{StateDir: tmp, ScopeKey: "scope-1", PrepPrompts: []string{"OVERRIDE"}, Runner: s}
	out, err := c.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if out.UsedRestore {
		t.Fatalf("did not expect restore when overrides present")
	}
	if s.calls != 1 || s.lastPrompt == "" {
		t.Fatalf("runner not called with prompt; calls=%d prompt=%q", s.calls, s.lastPrompt)
	}
}

func TestCoordinator_IgnoreRestoreWhenScopeMismatch(t *testing.T) {
	tmp := t.TempDir()
	_ = writeValidBundle(t, tmp, "scope-1")
	s := &stubRunner{resp: oai.ChatCompletionsResponse{Model: "m"}}
	c := &Coordinator{StateDir: tmp, ScopeKey: "other-scope", Runner: s}
	out, err := c.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if out.UsedRestore {
		t.Fatalf("expected live run due to scope mismatch")
	}
	if s.calls != 1 {
		t.Fatalf("runner should be called once, got %d", s.calls)
	}
}

func TestCoordinator_RefineForcesLiveRun(t *testing.T) {
	tmp := t.TempDir()
	_ = writeValidBundle(t, tmp, "s")
	s := &stubRunner{resp: oai.ChatCompletionsResponse{Model: "m"}}
	c := &Coordinator{StateDir: tmp, ScopeKey: "s", Refine: true, Runner: s}
	out, err := c.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if out.UsedRestore {
		t.Fatalf("expected live run when Refine=true")
	}
	if s.calls != 1 {
		t.Fatalf("runner should be called once, got %d", s.calls)
	}
}

func TestCoordinator_WarnsOnceWhenOverridesWithRefine(t *testing.T) {
	s := &stubRunner{resp: oai.ChatCompletionsResponse{Model: "m"}}
	var warns []string
	c := &Coordinator{Refine: true, PrepPrompts: []string{"OVR"}, Runner: s, Warnf: func(format string, args ...any) {
		warns = append(warns, fmt.Sprintf(format, args...))
	}}
	out, err := c.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if out.UsedRestore {
		t.Fatalf("should not restore when overrides present")
	}
	if len(warns) != 1 {
		t.Fatalf("expected exactly one warning, got %d: %#v", len(warns), warns)
	}
	if !strings.Contains(strings.ToLower(warns[0]), "override") || !strings.Contains(strings.ToLower(warns[0]), "refine") {
		t.Fatalf("warning should mention override and refine: %q", warns[0])
	}
}

func TestCoordinator_NoStateDirFallsBackToDefaultPrompt(t *testing.T) {
	s := &stubRunner{resp: oai.ChatCompletionsResponse{Model: "m"}}
	c := &Coordinator{Runner: s}
	out, err := c.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if out.UsedRestore || out.Restored != nil {
		t.Fatalf("should not use restore without state dir: %+v", out)
	}
	if s.calls != 1 || out.PromptUsed == "" {
		t.Fatalf("runner should be called with default prompt; calls=%d prompt=%q", s.calls, out.PromptUsed)
	}
}

func TestCoordinator_IgnoresCorruptStateAndRunsLive(t *testing.T) {
	tmp := t.TempDir()
	// Create corrupt latest.json
	if err := os.WriteFile(filepath.Join(tmp, "latest.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write latest.json: %v", err)
	}
	s := &stubRunner{resp: oai.ChatCompletionsResponse{Model: "m"}}
	c := &Coordinator{StateDir: tmp, Runner: s}
	out, err := c.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if out.UsedRestore {
		t.Fatalf("did not expect restore with corrupt state")
	}
	if s.calls != 1 {
		t.Fatalf("runner should be called once, got %d", s.calls)
	}
}

func TestCoordinator_PropagatesRunnerError(t *testing.T) {
	s := &stubRunner{err: errors.New("boom")}
	c := &Coordinator{Runner: s}
	_, err := c.Execute(context.Background())
	if err == nil {
		t.Fatalf("expected error from runner")
	}
}
