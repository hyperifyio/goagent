package testutil

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// BuildTool builds the named tool binary into a test-scoped temporary
// directory and returns the absolute path to the produced executable.
//
// Source discovery (absolute paths used to satisfy repository path hygiene
// rules in linters/tests):
//   - tools/cmd/<name> (canonical layout only)
func BuildTool(t *testing.T, name string) string {
	t.Helper()

	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("find repo root: %v", err)
	}

	// Determine binary name with OS suffix
	binName := name
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	outPath := filepath.Join(t.TempDir(), binName)

	// Candidate source locations (canonical layout only)
	var candidates []string
	candidates = append(candidates, filepath.Join(repoRoot, "tools", "cmd", name))

	var srcPath string
	for _, c := range candidates {
		if fi, statErr := os.Stat(c); statErr == nil {
			// Accept directories and regular files
			if fi.IsDir() || fi.Mode().IsRegular() {
				srcPath = c
				break
			}
		}
	}
	if srcPath == "" {
		t.Fatalf("tool sources not found for %q under %s", name, filepath.Join(repoRoot, "tools"))
	}

	cmd := exec.Command("go", "build", "-o", outPath, srcPath)
	cmd.Dir = repoRoot
	// Inherit environment; ensure CGO disabled for determinism
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s from %s failed: %v\n%s", name, relOrSame(repoRoot, srcPath), err, string(output))
	}
	return outPath
}

func findRepoRoot() (string, error) {
	// Start from CWD and walk up until go.mod is found
	start, err := os.Getwd()
	if err != nil || start == "" {
		return "", errors.New("cannot determine working directory")
	}
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s upward", start)
		}
		dir = parent
	}
}

func relOrSame(base, target string) string {
	if rel, err := filepath.Rel(base, target); err == nil {
		return rel
	}
	return target
}
