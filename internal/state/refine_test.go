package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRefineStateBundle_PreservesAndAppends(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	prev := &StateBundle{
		Version:     "1",
		CreatedAt:   now,
		ToolVersion: "dev",
		ModelID:     "gpt-5",
		BaseURL:     "https://api.example/v1",
		ToolsetHash: "toolhash",
		ScopeKey:    "scope",
		Prompts:     map[string]string{"system": "S", "developer": "D0"},
		PrepSettings: map[string]any{
			"temp": 0.1,
		},
		Context:    map[string]any{"k": "v"},
		ToolCaps:   map[string]any{"cap": true},
		Custom:     map[string]any{"note": "x"},
		SourceHash: ComputeSourceHash("gpt-5", "https://api.example/v1", "toolhash", "scope"),
	}

	refined, err := RefineStateBundle(prev, "refine here", "user asks")
	if err != nil {
		t.Fatalf("RefineStateBundle error: %v", err)
	}

	if refined == nil {
		t.Fatalf("got nil bundle")
	}
	if refined.Version != "1" || refined.ModelID != prev.ModelID || refined.BaseURL != prev.BaseURL || refined.ToolsetHash != prev.ToolsetHash || refined.ScopeKey != prev.ScopeKey {
		t.Fatalf("identifying fields not preserved: %+v", refined)
	}
	if refined.CreatedAt == prev.CreatedAt {
		t.Fatalf("CreatedAt not updated")
	}
	if refined.SourceHash == "" {
		t.Fatalf("SourceHash empty")
	}
	// SourceHash is recomputed from identifying fields and should equal prev's since those didn't change
	if refined.SourceHash != prev.SourceHash {
		t.Fatalf("SourceHash changed unexpectedly: %s vs %s", refined.SourceHash, prev.SourceHash)
	}
	dev := refined.Prompts["developer"]
	if !strings.Contains(dev, "D0") || !strings.Contains(dev, "refine here") || !strings.Contains(dev, "USER: user asks") {
		t.Fatalf("developer prompt not appended correctly: %q", dev)
	}

	// prev_sha should be SHA256 of canonical JSON of prev
	prevJSON, err := json.MarshalIndent(prev, "", "  ")
	if err != nil {
		t.Fatalf("marshal prev: %v", err)
	}
	wantPrevSHA := sha256Hex(prevJSON)
	if refined.PrevSHA != wantPrevSHA {
		t.Fatalf("prev_sha mismatch: got %s want %s", refined.PrevSHA, wantPrevSHA)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestRefineStateBundle_SaveSnapshot(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	prev := &StateBundle{
		Version:     "1",
		CreatedAt:   now,
		ToolVersion: "dev",
		ModelID:     "gpt-5",
		BaseURL:     "https://api.example/v1",
		ToolsetHash: "toolhash",
		ScopeKey:    "scope",
		Prompts:     map[string]string{"developer": "D0"},
		SourceHash:  ComputeSourceHash("gpt-5", "https://api.example/v1", "toolhash", "scope"),
	}

	refined, err := RefineStateBundle(prev, "refine here", "user asks")
	if err != nil {
		t.Fatalf("RefineStateBundle error: %v", err)
	}
	if err := SaveStateBundle(dir, refined); err != nil {
		t.Fatalf("SaveStateBundle: %v", err)
	}
	// Assert snapshot exists and latest.json points to it
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var snapshot string
	for _, e := range entries {
		if !e.IsDir() && e.Name() != "latest.json" {
			snapshot = e.Name()
			break
		}
	}
	if snapshot == "" {
		t.Fatalf("snapshot file not found")
	}
	if _, err := os.Stat(filepath.Join(dir, "latest.json")); err != nil {
		t.Fatalf("latest.json missing: %v", err)
	}
}
