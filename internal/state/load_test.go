package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func makeValidBundle(now string) *StateBundle {
	return &StateBundle{
		Version:     "1",
		CreatedAt:   now,
		ToolVersion: "test-1",
		ModelID:     "gpt-5",
		BaseURL:     "http://example.local",
		ToolsetHash: "abc",
		ScopeKey:    "scope-1",
		Prompts:     map[string]string{"system": "hi"},
		SourceHash:  ComputeSourceHash("gpt-5", "http://example.local", "abc", "scope-1"),
	}
}

func TestLoadLatestStateBundle_OK(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	b := makeValidBundle(now)
	if err := SaveStateBundle(dir, b); err != nil {
		t.Fatalf("SaveStateBundle: %v", err)
	}

	got, err := LoadLatestStateBundle(dir)
	if err != nil {
		t.Fatalf("LoadLatestStateBundle error: %v", err)
	}
	if got == nil {
		t.Fatalf("got nil bundle")
	}
	if got.Version != b.Version || got.CreatedAt != b.CreatedAt || got.ModelID != b.ModelID || got.BaseURL != b.BaseURL || got.ScopeKey != b.ScopeKey {
		t.Fatalf("loaded bundle mismatch: %+v vs %+v", got, b)
	}
}

func TestLoadLatestStateBundle_MissingLatest(t *testing.T) {
	dir := t.TempDir()
	if b, err := LoadLatestStateBundle(dir); !errors.Is(err, ErrStateInvalid) || b != nil {
		t.Fatalf("expected ErrStateInvalid and nil, got %v, %v", err, b)
	}
}

func TestLoadLatestStateBundle_CorruptLatest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write latest: %v", err)
	}
	if b, err := LoadLatestStateBundle(dir); !errors.Is(err, ErrStateInvalid) || b != nil {
		t.Fatalf("expected ErrStateInvalid and nil, got %v, %v", err, b)
	}
}

func TestLoadLatestStateBundle_UnknownVersion(t *testing.T) {
	dir := t.TempDir()
	// Write a valid snapshot first
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	b := makeValidBundle(now)
	if err := SaveStateBundle(dir, b); err != nil {
		t.Fatalf("SaveStateBundle: %v", err)
	}
	// Overwrite latest.json with version 2
	// Discover snapshot file name
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
	ptr := latestPointer{Version: "2", Path: snapshot, SHA256: "deadbeef"}
	data, err := json.Marshal(ptr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), data, 0o600); err != nil {
		t.Fatalf("write latest: %v", err)
	}

	if b2, err := LoadLatestStateBundle(dir); !errors.Is(err, ErrStateInvalid) || b2 != nil {
		t.Fatalf("expected ErrStateInvalid and nil, got %v, %v", err, b2)
	}
}

func TestLoadLatestStateBundle_MissingSnapshot(t *testing.T) {
	dir := t.TempDir()
	// Write pointer to missing file
	ptr := latestPointer{Version: "1", Path: "missing.json", SHA256: ""}
	data, err := json.Marshal(ptr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), data, 0o600); err != nil {
		t.Fatalf("write latest: %v", err)
	}
	if b, err := LoadLatestStateBundle(dir); !errors.Is(err, ErrStateInvalid) || b != nil {
		t.Fatalf("expected ErrStateInvalid and nil, got %v, %v", err, b)
	}
}

func TestLoadLatestStateBundle_PermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows permissions semantics differ")
	}
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	b := makeValidBundle(now)
	if err := SaveStateBundle(dir, b); err != nil {
		t.Fatalf("SaveStateBundle: %v", err)
	}
	// Find snapshot and chmod to 000 to induce EPERM when reading
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
	snapPath := filepath.Join(dir, snapshot)
	if err := os.Chmod(snapPath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(snapPath, 0o600); err != nil {
			t.Logf("ignored chmod restore error: %v", err)
		}
	})

	if b2, err := LoadLatestStateBundle(dir); !errors.Is(err, ErrStateInvalid) || b2 != nil {
		t.Fatalf("expected ErrStateInvalid and nil, got %v, %v", err, b2)
	}
}
