package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSaveStateBundle_WritesFilesAtomicallyWithPermsAndPointer(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()

	// Track whether syncDirFunc was called
	calledSync := false
	syncDirFunc = func(dir string) error {
		calledSync = true
		f, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer func() {
			if err := f.Close(); err != nil {
				t.Logf("ignored close error: %v", err)
			}
		}()
		return f.Sync()
	}
	t.Cleanup(func() {
		syncDirFunc = func(dir string) error {
			f, err := os.Open(dir)
			if err != nil {
				return err
			}
			defer func() {
				if err := f.Close(); err != nil {
					t.Logf("ignored close error: %v", err)
				}
			}()
			return f.Sync()
		}
	})

	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	b := &StateBundle{
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

	// Act
	if err := SaveStateBundle(tempDir, b); err != nil {
		t.Fatalf("SaveStateBundle error: %v", err)
	}

	// Assert snapshot exists
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var snapshot string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "state-") && strings.HasSuffix(e.Name(), ".json") {
			snapshot = e.Name()
			break
		}
	}
	if snapshot == "" {
		t.Fatalf("snapshot file not found in %s", tempDir)
	}

	// Check file mode 0600
	info, err := os.Stat(filepath.Join(tempDir, snapshot))
	if err != nil {
		t.Fatalf("stat snapshot: %v", err)
	}
	if runtime.GOOS != "windows" { // Windows has different mode semantics
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("snapshot perms = %v, want 0600", info.Mode().Perm())
		}
	}

	// Check latest.json exists and points to snapshot
	latestPath := filepath.Join(tempDir, "latest.json")
	latestBytes, err := os.ReadFile(latestPath)
	if err != nil {
		t.Fatalf("read latest.json: %v", err)
	}
	var ptr latestPointer
	if err := json.Unmarshal(latestBytes, &ptr); err != nil {
		t.Fatalf("unmarshal latest.json: %v", err)
	}
	if ptr.Version != "1" {
		t.Fatalf("pointer version = %q, want 1", ptr.Version)
	}
	if ptr.Path != snapshot {
		t.Fatalf("pointer path = %q, want %q", ptr.Path, snapshot)
	}
	if ptr.SHA256 == "" {
		t.Fatalf("pointer sha256 empty")
	}

	if !calledSync {
		t.Fatalf("expected directory fsync to be called")
	}
}

func TestSaveStateBundle_InvalidBundle(t *testing.T) {
	tempDir := t.TempDir()
	// Missing required fields (CreatedAt invalid)
	b := &StateBundle{Version: "1", CreatedAt: "not-time", ModelID: "m", BaseURL: "u", ScopeKey: "s"}
	if err := SaveStateBundle(tempDir, b); err == nil {
		t.Fatalf("expected error for invalid bundle")
	}
}

func TestSaveStateBundle_SanitizesSecretsAndRejectsInsecureDir(t *testing.T) {
    tempDir := t.TempDir()
    // Make directory world-writable on Unix to trigger rejection; skip on Windows
    if runtime.GOOS != "windows" {
        if err := os.Chmod(tempDir, 0o707); err != nil {
            t.Fatalf("chmod: %v", err)
        }
    }

    now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
    b := &StateBundle{
        Version:     "1",
        CreatedAt:   now,
        ToolVersion: "test-1",
        ModelID:     "gpt-5",
        BaseURL:     "http://example.local",
        ToolsetHash: "abc",
        ScopeKey:    "scope-1",
        Prompts:     map[string]string{"system": "Authorization: Bearer secretTOKEN1234567890"},
        PrepSettings: map[string]any{
            "api_key": "sk-verylongexamplekey-ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
            "request_body": "{ big raw body }",
        },
        SourceHash: ComputeSourceHash("gpt-5", "http://example.local", "abc", "scope-1"),
    }

    err := SaveStateBundle(tempDir, b)
    if runtime.GOOS != "windows" {
        if err == nil {
            t.Fatalf("expected error for insecure dir perms")
        }
        // Fix perms and try again
        if err := os.Chmod(tempDir, 0o700); err != nil {
            t.Fatalf("chmod fix: %v", err)
        }
    }

    if err := SaveStateBundle(tempDir, b); err != nil {
        t.Fatalf("SaveStateBundle error after fix: %v", err)
    }
    // Read snapshot and verify redactions present
    entries, _ := os.ReadDir(tempDir)
    var snapshot string
    for _, e := range entries {
        if strings.HasPrefix(e.Name(), "state-") && strings.HasSuffix(e.Name(), ".json") {
            snapshot = filepath.Join(tempDir, e.Name())
            break
        }
    }
    if snapshot == "" {
        t.Fatalf("snapshot not found")
    }
    data, err := os.ReadFile(snapshot)
    if err != nil {
        t.Fatalf("read snapshot: %v", err)
    }
    s := string(data)
    if strings.Contains(s, "secretTOKEN1234567890") {
        t.Fatalf("authorization token not redacted: %s", s)
    }
    if !strings.Contains(s, "Authorization: Bearer ****") {
        t.Fatalf("authorization scheme not preserved/redacted: %s", s)
    }
    if strings.Contains(s, "sk-verylongexamplekey") {
        t.Fatalf("api key not redacted: %s", s)
    }
    if strings.Contains(s, "{ big raw body }") {
        t.Fatalf("raw body not omitted: %s", s)
    }
}
