package main

import (
    "encoding/json"
    "fmt"
    "io"
    "math/rand"
    "os"
    "path/filepath"
    "strings"
)

// printStateDryRunPlan outputs a concise plan describing intended state actions.
// It never writes to disk. Exit code 0 on success.
func printStateDryRunPlan(cfg cliConfig, stdout io.Writer, stderr io.Writer) int {
	// Normalize/expand state-dir as parseFlags would have done
	dir := strings.TrimSpace(cfg.stateDir)
	if dir != "" {
		if strings.HasPrefix(dir, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
			}
		}
		dir = filepath.Clean(dir)
	}

	// Determine action
	type plan struct {
		Action        string `json:"action"`
		StateDir      string `json:"state_dir"`
		ScopeKey      string `json:"scope_key"`
		Refine        bool   `json:"refine"`
		HasRefineText bool   `json:"has_refine_text"`
		HasRefineFile bool   `json:"has_refine_file"`
		Notes         string `json:"notes"`
	}
	p := plan{StateDir: dir, ScopeKey: strings.TrimSpace(cfg.stateScope), Refine: cfg.stateRefine, HasRefineText: strings.TrimSpace(cfg.stateRefineText) != "", HasRefineFile: strings.TrimSpace(cfg.stateRefineFile) != ""}

	if dir == "" {
		p.Action = "none"
		p.Notes = "state-dir not set; no restore/save will occur"
	} else if cfg.stateRefine || p.HasRefineText || p.HasRefineFile {
		p.Action = "refine"
		p.Notes = "would load latest bundle (if any), apply refinement, and write a new snapshot"
	} else {
		// Not refining: would attempt restore-before-prep and save afterward
		p.Action = "restore_or_save"
		p.Notes = "would attempt restore-before-prep using latest.json; on success reuse without calling pre-stage; otherwise would run pre-stage and save a new snapshot"
	}

	// Include a synthetic SHA hint to demonstrate formatting without real IO
	// This keeps output stable yet obviously a placeholder.
	hint := map[string]any{
		"sample_short_sha": fmt.Sprintf("%08x", rand.Uint32()),
	}
	out := map[string]any{
		"plan": p,
		"hint": hint,
	}
	if b, err := json.MarshalIndent(out, "", "  "); err == nil {
		safeFprintln(stdout, string(b))
		return 0
	}
	safeFprintln(stdout, "{\"plan\":{\"action\":\"unknown\"}}")
	return 0
}
