package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fsApplyPatchOutput struct {
	FilesChanged int `json:"filesChanged"`
}

// buildFsApplyPatch builds ./tools/fs_apply_patch into a temp binary
func buildFsApplyPatch(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "fs-apply-patch")
	cmd := exec.Command("go", "build", "-o", bin, "./fs_apply_patch")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fs_apply_patch: %v\n%s", err, string(out))
	}
	return bin
}

func runFsApplyPatch(t *testing.T, bin string, input any) (fsApplyPatchOutput, string, int) {
	t.Helper()
	data, _ := json.Marshal(input)
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	var out fsApplyPatchOutput
	_ = json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &out)
	return out, stderr.String(), code
}

func runFsApplyPatchInDir(t *testing.T, bin, dir string, input any) (fsApplyPatchOutput, string, int) {
    t.Helper()
    data, _ := json.Marshal(input)
    cmd := exec.Command(bin)
    cmd.Dir = dir
    cmd.Stdin = bytes.NewReader(data)
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    err := cmd.Run()
    code := 0
    if err != nil {
        if ee, ok := err.(*exec.ExitError); ok {
            code = ee.ExitCode()
        } else {
            code = 1
        }
    }
    var out fsApplyPatchOutput
    _ = json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &out)
    return out, stderr.String(), code
}

func TestFsApplyPatch_CleanApply_NewFile(t *testing.T) {
    bin := buildFsApplyPatch(t)
    // Prepare a simple unified diff creating a file
    diff := "" +
        "--- /dev/null\n" +
        "+++ b/tmp_new_file.txt\n" +
        "@@ -0,0 +1,2 @@\n" +
        "+hello\n" +
        "+world\n"

    // Run in an isolated temp directory to avoid polluting the repo
    work := t.TempDir()
    out, stderr, code := runFsApplyPatchInDir(t, bin, work, map[string]any{
        "unifiedDiff": diff,
    })
    if code == 0 {
        // Once implemented, expect code==0 and filesChanged==1
        if out.FilesChanged != 1 {
            t.Fatalf("filesChanged mismatch, got %d want 1", out.FilesChanged)
        }
        if _, err := os.Stat(filepath.Join(work, "tmp_new_file.txt")); err != nil {
            t.Fatalf("expected file to exist: %v", err)
        }
        return
    }
    // For the initial stub, ensure we get a structured error
    if !strings.Contains(strings.ToUpper(stderr), "NOT_IMPLEMENTED") {
        t.Fatalf("expected NOT_IMPLEMENTED in stderr, got %q", stderr)
    }
}

func TestFsApplyPatch_CleanApply_NewFile_Succeeds(t *testing.T) {
    bin := buildFsApplyPatch(t)
    work := t.TempDir()
    diff := "" +
        "--- /dev/null\n" +
        "+++ b/tmp_new_file.txt\n" +
        "@@ -0,0 +1,2 @@\n" +
        "+hello\n" +
        "+world\n"

    out, stderr, code := runFsApplyPatchInDir(t, bin, work, map[string]any{
        "unifiedDiff": diff,
    })
    if code != 0 {
        t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
    }
    if out.FilesChanged != 1 {
        t.Fatalf("filesChanged mismatch, got %d want 1", out.FilesChanged)
    }
    if _, err := os.Stat(filepath.Join(work, "tmp_new_file.txt")); err != nil {
        t.Fatalf("expected file to exist: %v", err)
    }
}

func TestFsApplyPatch_Idempotent_NewFile(t *testing.T) {
    bin := buildFsApplyPatch(t)
    work := t.TempDir()
    diff := "" +
        "--- /dev/null\n" +
        "+++ b/tmp_new_file.txt\n" +
        "@@ -0,0 +1,2 @@\n" +
        "+hello\n" +
        "+world\n"

    // First apply should create the file
    out1, stderr1, code1 := runFsApplyPatchInDir(t, bin, work, map[string]any{
        "unifiedDiff": diff,
    })
    if code1 != 0 {
        t.Fatalf("first apply expected success, got exit=%d stderr=%q", code1, stderr1)
    }
    if out1.FilesChanged != 1 {
        t.Fatalf("first apply filesChanged mismatch, got %d want 1", out1.FilesChanged)
    }
    // Second apply of the same diff should be idempotent: no-op with success
    out2, stderr2, code2 := runFsApplyPatchInDir(t, bin, work, map[string]any{
        "unifiedDiff": diff,
    })
    if code2 != 0 {
        t.Fatalf("second apply expected success, got exit=%d stderr=%q", code2, stderr2)
    }
    if out2.FilesChanged != 0 {
        t.Fatalf("second apply filesChanged mismatch, got %d want 0", out2.FilesChanged)
    }
}

func TestFsApplyPatch_Conflict_TargetExistsWithDifferentContent(t *testing.T) {
    bin := buildFsApplyPatch(t)
    work := t.TempDir()

    // Pre-create target with different content
    if err := os.WriteFile(filepath.Join(work, "tmp_new_file.txt"), []byte("different\ncontent\n"), 0o644); err != nil {
        t.Fatalf("prep write: %v", err)
    }

    // Diff attempts to create a new file with different content (new-file hunk)
    diff := "" +
        "--- /dev/null\n" +
        "+++ b/tmp_new_file.txt\n" +
        "@@ -0,0 +1,2 @@\n" +
        "+hello\n" +
        "+world\n"

    out, stderr, code := runFsApplyPatchInDir(t, bin, work, map[string]any{
        "unifiedDiff": diff,
    })
    if code == 0 {
        t.Fatalf("expected failure, got success filesChanged=%d", out.FilesChanged)
    }
    if !strings.Contains(strings.ToLower(stderr), "target exists") {
        t.Fatalf("expected error mentioning target exists, got %q", stderr)
    }
}
