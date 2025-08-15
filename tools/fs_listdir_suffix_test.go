package main

import (
    "runtime"
    "strings"
    "testing"

    testutil "github.com/hyperifyio/goagent/tools/testutil"
)

// TestBuildToolSuffix_Representative validates the suffix behavior via the shared helper.
func TestBuildToolSuffix_Representative(t *testing.T) {
    p := testutil.BuildTool(t, "fs_listdir")
    if runtime.GOOS == "windows" {
        if !strings.HasSuffix(p, ".exe") {
            t.Fatalf("expected Windows suffix .exe in path: %q", p)
        }
    } else {
        if strings.HasSuffix(p, ".exe") {
            t.Fatalf("unexpected .exe suffix on non-Windows: %q", p)
        }
    }
}
