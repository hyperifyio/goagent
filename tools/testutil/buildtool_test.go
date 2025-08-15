package testutil

import (
    "runtime"
    "strings"
    "testing"
)

func TestBuildTool_WindowsSuffix(t *testing.T) {
    // Use a real tool name to ensure build succeeds across environments.
    path := BuildTool(t, "fs_listdir")
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
