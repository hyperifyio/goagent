package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestLegacyPrintConfigDefaults_NoNewFlags ensures that a minimal invocation
// without new flags produces the same resolved config baseline.
func TestLegacyPrintConfigDefaults_NoNewFlags(t *testing.T) {
	// Ensure env does not influence defaults
	t.Setenv("OAI_BASE_URL", "")
	t.Setenv("OAI_MODEL", "")
	t.Setenv("OAI_HTTP_TIMEOUT", "")
	t.Setenv("OAI_IMAGE_MODEL", "")

	var out, err bytes.Buffer
	code := cliMain([]string{"-prompt", "p", "-print-config"}, &out, &err)
	if code != 0 {
		t.Fatalf("print-config exit=%d, stderr=%s", code, err.String())
	}
	// Parse JSON
	var payload map[string]any
	if jerr := json.Unmarshal(out.Bytes(), &payload); jerr != nil {
		t.Fatalf("unmarshal print-config: %v; got %s", jerr, out.String())
	}
	// Top-level expectations
	if got, ok := payload["model"].(string); !ok || got != "oss-gpt-20b" {
		t.Fatalf("model=%v; want oss-gpt-20b", payload["model"])
	}
	if got, ok := payload["baseURL"].(string); !ok || got != "https://api.openai.com/v1" {
		t.Fatalf("baseURL=%v; want https://api.openai.com/v1", payload["baseURL"])
	}
	if got, ok := payload["httpTimeout"].(string); !ok || got != "30s" {
		t.Fatalf("httpTimeout=%v; want 30s", payload["httpTimeout"])
	}
	// Image block expectations
	img, ok := payload["image"].(map[string]any)
	if !ok {
		t.Fatalf("missing image block in print-config")
	}
	if got, ok := img["model"].(string); !ok || got != "gpt-image-1" {
		t.Fatalf("image.model=%v; want gpt-image-1", img["model"])
	}
}

func TestConflictingPromptSources_ErrorMessage(t *testing.T) {
	var out, err bytes.Buffer
	code := cliMain([]string{"-prompt", "p", "-prompt-file", os.DevNull}, &out, &err)
	if code != 2 {
		t.Fatalf("exit=%d; want 2", code)
	}
	if !strings.Contains(err.String(), "-prompt and -prompt-file are mutually exclusive") {
		t.Fatalf("stderr did not contain conflict message; got: %s", err.String())
	}
}

func TestConflictingSystemSources_ErrorMessage(t *testing.T) {
	var out, err bytes.Buffer
	// Provide both -system (non-default) and -system-file
	code := cliMain([]string{"-prompt", "p", "-system", "X", "-system-file", os.DevNull}, &out, &err)
	if code != 2 {
		t.Fatalf("exit=%d; want 2", code)
	}
	if !strings.Contains(err.String(), "-system and -system-file are mutually exclusive") {
		t.Fatalf("stderr did not contain system conflict message; got: %s", err.String())
	}
}

func TestLoadMessagesWithPromptConflict_ErrorMessage(t *testing.T) {
	var out, err bytes.Buffer
	code := cliMain([]string{"-load-messages", os.DevNull, "-prompt", "p"}, &out, &err)
	if code != 2 {
		t.Fatalf("exit=%d; want 2", code)
	}
	if !strings.Contains(err.String(), "-load-messages cannot be combined with -prompt or -prompt-file") {
		t.Fatalf("stderr did not contain load/prompt conflict message; got: %s", err.String())
	}
}

func TestSaveAndLoadMessagesConflict_ErrorMessage(t *testing.T) {
	var out, err bytes.Buffer
	code := cliMain([]string{"-prompt", "p", "-save-messages", os.DevNull, "-load-messages", os.DevNull}, &out, &err)
	if code != 2 {
		t.Fatalf("exit=%d; want 2", code)
	}
	if !strings.Contains(err.String(), "-save-messages and -load-messages are mutually exclusive") {
		t.Fatalf("stderr did not contain save/load conflict message; got: %s", err.String())
	}
}
