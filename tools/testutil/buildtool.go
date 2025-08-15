package testutil

import (
    "path/filepath"
    "runtime"
    "testing"
)

// BuildTool returns a deterministic binary path for the given tool name under
// the test's temporary directory, applying the correct OS-specific suffix.
// A future slice will extend this to actually build from ./tools/cmd/<name>.
func BuildTool(t *testing.T, name string) string {
    t.Helper()
    binName := name
    if runtime.GOOS == "windows" {
        binName += ".exe"
    }
    return filepath.Join(t.TempDir(), binName)
}
