//nolint:errcheck // Tests elide error checks on JSON encoders/decoders where not relevant to the assertion under test.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hyperifyio/goagent/tools/testutil"
)

func buildTool(t *testing.T) string {
	// Build this package into a temp binary
	bin := filepath.Join(t.TempDir(), "img_create-test-bin")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, string(out))
	}
	return bin
}

func runTool(t *testing.T, bin string, in any, env map[string]string) (stdout, stderr string, code int) {
	data, _ := json.Marshal(in)
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(data)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if env != nil {
		e := os.Environ()
		for k, v := range env {
			e = append(e, k+"="+v)
		}
		cmd.Env = e
	}
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	return outBuf.String(), errBuf.String(), code
}

func TestMissingPrompt(t *testing.T) {
	bin := buildTool(t)
	_, stderr, code := runTool(t, bin, map[string]any{}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "prompt is required") {
		t.Fatalf("expected prompt error, got %q", stderr)
	}
}

func TestHappyPath_SaveOnePNG(t *testing.T) {
	// 1x1 transparent PNG
	png1x1 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO9cFmgAAAAASUVORK5CYII="
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			Model      string `json:"model"`
			Prompt     string `json:"prompt"`
			N          int    `json:"n"`
			Size       string `json:"size"`
			RespFmt    string `json:"response_format"`
			Background string `json:"background"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		if req.Model != "gpt-image-1" || req.Prompt != "tiny-pixel" || req.N != 1 || req.Size != "1024x1024" || req.RespFmt != "b64_json" {
			t.Fatalf("unexpected payload: %+v", req)
		}
		if req.Background != "transparent" {
			t.Fatalf("expected extras merged: background=transparent, got %q", req.Background)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"b64_json": png1x1}},
			"model": "gpt-image-1",
		})
	}))
	defer srv.Close()

	bin := buildTool(t)
	outDir := testutil.MakeRepoRelTempDir(t, "imgcreate-out-")
	stdout, stderr, code := runTool(t, bin, map[string]any{
		"prompt": "tiny-pixel",
		"save":   map[string]any{"dir": outDir, "basename": "img", "ext": "png"},
		"extras": map[string]any{"background": "transparent"},
	}, map[string]string{
		"OAI_IMAGE_BASE_URL": srv.URL,
		"OAI_API_KEY":        "test-123",
	})
	if code != 0 {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	var obj struct {
		Saved []struct {
			Path   string `json:"path"`
			Bytes  int    `json:"bytes"`
			Sha256 string `json:"sha256"`
		} `json:"saved"`
		N     int    `json:"n"`
		Size  string `json:"size"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &obj); err != nil {
		t.Fatalf("bad stdout json: %v; raw=%q", err, stdout)
	}
	if obj.N != 1 || len(obj.Saved) != 1 {
		t.Fatalf("unexpected saved count: %+v", obj)
	}
	// Verify file exists and bytes match decoded b64
	got, err := os.ReadFile(obj.Saved[0].Path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	want, _ := base64.StdEncoding.DecodeString(png1x1)
	if len(got) != len(want) {
		t.Fatalf("bytes mismatch: got %d want %d", len(got), len(want))
	}
}

func TestExtras_DoNotOverrideCoreKeys_AndSanitize(t *testing.T) {
	// Server returns a trivial valid image
	png1x1 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO9cFmgAAAAASUVORK5CYII="
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		captured = req
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"b64_json": png1x1}},
		})
	}))
	defer srv.Close()

	bin := buildTool(t)
	outDir := testutil.MakeRepoRelTempDir(t, "imgcreate-out-")
	_, stderr, code := runTool(t, bin, map[string]any{
		"prompt": "tiny",
		"n":      2,
		"size":   "512x512",
		"model":  "gpt-image-1",
		"save":   map[string]any{"dir": outDir},
		"extras": map[string]any{
			"prompt":          "OVERRIDE-ATTEMPT",
			"n":               99,
			"size":            "2048x2048",
			"response_format": "raw",
			"ok_string":       "yes",
			"ok_number":       1.5,
			"ok_bool":         true,
			"drop_obj":        map[string]any{"x": 1},
			"drop_arr":        []any{1, 2, 3},
		},
	}, map[string]string{
		"OAI_IMAGE_BASE_URL": srv.URL,
	})
	if code != 0 {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	// Core keys must remain as provided in top-level fields
	if captured["prompt"] != "tiny" || captured["n"].(float64) != 2 || captured["size"] != "512x512" || captured["response_format"] != "b64_json" {
		t.Fatalf("core keys overridden by extras: %+v", captured)
	}
	if captured["ok_string"] != "yes" || captured["ok_bool"] != true {
		t.Fatalf("expected sanitized primitives present: %+v", captured)
	}
	if _, ok := captured["drop_obj"]; ok {
		t.Fatalf("unexpected object in extras: %+v", captured)
	}
	if _, ok := captured["drop_arr"]; ok {
		t.Fatalf("unexpected array in extras: %+v", captured)
	}
}

func TestMissingSaveDir_WhenReturnB64False(t *testing.T) {
	bin := buildTool(t)
	// Default return_b64 is false; omit save to trigger validation error
	_, stderr, code := runTool(t, bin, map[string]any{
		"prompt": "tiny",
	}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "save.dir is required when return_b64=false") {
		t.Fatalf("expected save.dir error, got %q", stderr)
	}
}

func TestInvalidSizePattern(t *testing.T) {
	bin := buildTool(t)
	// Provide an invalid size and set return_b64 to bypass save requirements
	_, stderr, code := runTool(t, bin, map[string]any{
		"prompt":     "tiny",
		"size":       "big",
		"return_b64": true,
	}, nil)
	if code == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "size must match") {
		t.Fatalf("expected size pattern error, got %q", stderr)
	}
}

func TestAPI400_JSONErrorIsSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "bad prompt"},
		})
	}))
	defer srv.Close()

	bin := buildTool(t)
	outDir := testutil.MakeRepoRelTempDir(t, "imgcreate-out-")
	_, stderr, code := runTool(t, bin, map[string]any{
		"prompt": "tiny",
		"save":   map[string]any{"dir": outDir, "basename": "img", "ext": "png"},
	}, map[string]string{
		"OAI_IMAGE_BASE_URL": srv.URL,
		"OAI_API_KEY":        "test-123",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if !strings.Contains(stderr, "bad prompt") {
		t.Fatalf("expected API error message surfaced, got %q", stderr)
	}
}

func TestSaveDir_AbsolutePathRejected(t *testing.T) {
	bin := buildTool(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	abs := filepath.Join(wd, "imgcreate-abs-out")
	// Ensure absolute path
	if !filepath.IsAbs(abs) {
		t.Fatalf("expected absolute path, got %q", abs)
	}
	_, stderr, code := runTool(t, bin, map[string]any{
		"prompt": "tiny",
		"save":   map[string]any{"dir": abs},
	}, map[string]string{
		"OAI_IMAGE_BASE_URL": "http://127.0.0.1:9", // invalid to avoid real network if it would try
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit for absolute save.dir")
	}
	if !strings.Contains(stderr, "repo-relative") {
		t.Fatalf("expected repo-relative error, got %q", stderr)
	}
}

func TestSaveDir_EscapeOutsideRepoRejected(t *testing.T) {
	bin := buildTool(t)
	_, stderr, code := runTool(t, bin, map[string]any{
		"prompt": "tiny",
		"save":   map[string]any{"dir": filepath.Clean(filepath.Join(".."))},
	}, map[string]string{
		"OAI_IMAGE_BASE_URL": "http://127.0.0.1:9",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit for escape path")
	}
	if !strings.Contains(stderr, "escapes repository root") {
		t.Fatalf("expected escape error, got %q", stderr)
	}
}

func TestSaveDir_CleansRelativeAndCreatesNested_WithSHA256(t *testing.T) {
	// 1x1 transparent PNG bytes and known SHA256
	png1x1 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO9cFmgAAAAASUVORK5CYII="
	wantBytes, _ := base64.StdEncoding.DecodeString(png1x1)
	sum := sha256.Sum256(wantBytes)
	wantSHA := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"b64_json": png1x1}},
			"model": "gpt-image-1",
		})
	}))
	defer srv.Close()

	bin := buildTool(t)
	base := testutil.MakeRepoRelTempDir(t, "imgcreate-nested-")
	// Provide a dir that cleans to a simple child (e.g., a/../b -> b)
	nested := filepath.Join(base, "a", "..", "b")
	stdout, stderr, code := runTool(t, bin, map[string]any{
		"prompt": "tiny-pixel",
		"save":   map[string]any{"dir": nested, "basename": "img"},
	}, map[string]string{
		"OAI_IMAGE_BASE_URL": srv.URL,
		"OAI_API_KEY":        "test-123",
	})
	if code != 0 {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	var obj struct {
		Saved []struct {
			Path   string `json:"path"`
			Bytes  int    `json:"bytes"`
			Sha256 string `json:"sha256"`
		} `json:"saved"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &obj); err != nil {
		t.Fatalf("bad stdout json: %v; raw=%q", err, stdout)
	}
	if len(obj.Saved) != 1 {
		t.Fatalf("expected one saved file, got %d", len(obj.Saved))
	}
	if obj.Saved[0].Sha256 != wantSHA {
		t.Fatalf("sha256 mismatch: got %s want %s", obj.Saved[0].Sha256, wantSHA)
	}
	// Path should be repo-relative (not absolute)
	if filepath.IsAbs(obj.Saved[0].Path) {
		t.Fatalf("expected relative saved path, got absolute: %q", obj.Saved[0].Path)
	}
	// Ensure nested directories were created; file exists
	if _, err := os.Stat(obj.Saved[0].Path); err != nil {
		t.Fatalf("stat saved file: %v", err)
	}
}

func TestBasename_MustNotContainPathSeparators(t *testing.T) {
	// Need a server so the tool reaches save-path validation after decoding
	png1x1 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO9cFmgAAAAASUVORK5CYII="
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"b64_json": png1x1}},
		})
	}))
	defer srv.Close()

	bin := buildTool(t)
	outDir := testutil.MakeRepoRelTempDir(t, "imgcreate-out-")
	badBase := "bad/name"
	// On Windows also try using backslash explicitly
	if runtime.GOOS == "windows" {
		badBase = "bad\\name"
	}
	_, stderr, code := runTool(t, bin, map[string]any{
		"prompt":     "tiny",
		"save":       map[string]any{"dir": outDir, "basename": badBase},
		"return_b64": false,
	}, map[string]string{
		"OAI_IMAGE_BASE_URL": srv.URL,
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit for basename with separator")
	}
	if !strings.Contains(stderr, "basename must not contain path separators") {
		t.Fatalf("expected basename separator error, got %q", stderr)
	}
}
