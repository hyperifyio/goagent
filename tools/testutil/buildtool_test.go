package testutil

import (
    "runtime"
    "strings"
    "testing"
)

func TestBuildTool_WindowsSuffix(t *testing.T) {
    path := BuildTool(t, "demo")
    if runtime.GOOS == "windows" {
        if !strings.HasSuffix(path, ".exe") {
            t.Fatalf("expected .exe suffix on Windows, got %q", path)
        }
    } else {
        if strings.HasSuffix(path, ".exe") {
            t.Fatalf("did not expect .exe suffix on non-Windows, got %q", path)
        }
    }
}
