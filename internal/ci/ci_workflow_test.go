package ci

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

// This test asserts two things locally without requiring CI:
// 1) Makefile lint recipe runs check-go-version before invoking golangci-lint
// 2) The CI workflow includes an explicit step that verifies the ordering
func TestLintOrderLocallyAndInWorkflow(t *testing.T) {
    repoRoot, err := os.Getwd()
    if err != nil {
        t.Fatalf("getwd: %v", err)
    }

    // Assert Makefile ordering: check-go-version appears before golangci-lint
    mkPath := filepath.Join(repoRoot, "..", "..", "Makefile")
    mkBytes, err := os.ReadFile(mkPath)
    if err != nil {
        t.Fatalf("read Makefile: %v", err)
    }
    mk := string(mkBytes)
    if !strings.Contains(mk, "lint:") {
        t.Fatalf("Makefile missing 'lint:' target")
    }
    // Extract only the lint recipe block (the lines starting with a tab after 'lint:')
    lines := strings.Split(mk, "\n")
    lintIdx := -1
    for i, ln := range lines {
        if strings.HasPrefix(ln, "lint:") {
            lintIdx = i
            break
        }
    }
    if lintIdx < 0 {
        t.Fatalf("Makefile missing lint target label")
    }
    var recipeLines []string
    for j := lintIdx + 1; j < len(lines); j++ {
        ln := lines[j]
        if strings.HasPrefix(ln, "\t") { // recipe lines start with a tab
            recipeLines = append(recipeLines, ln)
            continue
        }
        // Stop when we hit the next non-recipe line (new target or blank without tab)
        if strings.TrimSpace(ln) == "" {
            // allow empty recipe line with tab only
            if strings.HasPrefix(ln, "\t") {
                recipeLines = append(recipeLines, ln)
                continue
            }
        }
        // Not a recipe line: end of recipe
        break
    }
    recipe := strings.Join(recipeLines, "\n")
    idxCheck := strings.Index(recipe, "check-go-version")
    if idxCheck < 0 {
        t.Fatalf("lint recipe missing 'check-go-version' invocation")
    }
    idxGcl := strings.Index(recipe, "golangci-lint")
    if idxGcl < 0 {
        t.Fatalf("lint recipe missing 'golangci-lint' invocation")
    }
    if !(idxCheck < idxGcl) {
        t.Fatalf("expected check-go-version to run before golangci-lint inside lint recipe (idx %d < %d)", idxCheck, idxGcl)
    }

    // Assert CI workflow includes the lint order assertion step
    wfPath := filepath.Join(repoRoot, "..", "..", ".github", "workflows", "ci.yml")
    wfBytes, err := os.ReadFile(wfPath)
    if err != nil {
        t.Fatalf("read ci workflow: %v", err)
    }
    wf := string(wfBytes)
    if !strings.Contains(wf, "lint (includes check-go-version)") {
        t.Fatalf("workflow missing explicit lint step name indicating check-go-version inclusion")
    }
    if !strings.Contains(wf, "Assert lint order (check-go-version before golangci-lint)") {
        t.Fatalf("workflow missing order assertion step")
    }
}
