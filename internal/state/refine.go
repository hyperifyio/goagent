package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// RefineStateBundle produces a new StateBundle derived from prev by applying a
// deterministic refinement. It preserves unspecified fields, updates CreatedAt
// to the current UTC time (RFC3339), recomputes SourceHash, and records PrevSHA
// as the SHA-256 (hex) of the canonical JSON of the previous bundle.
//
// The refinement strategy is intentionally simple and deterministic:
//   - Prompts["developer"] is appended with two new paragraphs that include the
//     refineInput and userPrompt, separated by blank lines.
//   - Other fields are preserved as-is.
func RefineStateBundle(prev *StateBundle, refineInput string, userPrompt string) (*StateBundle, error) {
	if prev == nil {
		return nil, errors.New("nil prev")
	}
	if err := prev.Validate(); err != nil {
		return nil, err
	}

	// Compute prev SHA over the same canonical form we persist (indent to match SaveStateBundle)
	prevJSON, err := json.MarshalIndent(prev, "", "  ")
	if err != nil {
		return nil, err
	}
	prevSum := sha256.Sum256(prevJSON)
	prevSHAHex := hex.EncodeToString(prevSum[:])

	// Deep-ish copy helpers
	cloneStrMap := func(in map[string]string) map[string]string {
		if in == nil {
			return nil
		}
		out := make(map[string]string, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	cloneAnyMap := func(in map[string]any) map[string]any {
		if in == nil {
			return nil
		}
		out := make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}

	prompts := cloneStrMap(prev.Prompts)
	if prompts == nil {
		prompts = make(map[string]string)
	}

	// Append deterministic refinement note to developer prompt
	dev := prompts["developer"]
	var parts []string
	if strings.TrimSpace(dev) != "" {
		parts = append(parts, dev)
	}
	if strings.TrimSpace(refineInput) != "" {
		parts = append(parts, refineInput)
	}
	if strings.TrimSpace(userPrompt) != "" {
		parts = append(parts, "USER: "+userPrompt)
	}
	prompts["developer"] = strings.TrimSpace(strings.Join(parts, "\n\n"))

	// Timestamp: ensure it advances at least by 1s if equal to previous
	now := time.Now().UTC().Truncate(time.Second)
	if prev.CreatedAt == now.Format(time.RFC3339) {
		now = now.Add(time.Second)
	}

	newBundle := &StateBundle{
		Version:      prev.Version,
		CreatedAt:    now.Format(time.RFC3339),
		ToolVersion:  prev.ToolVersion,
		ModelID:      prev.ModelID,
		BaseURL:      prev.BaseURL,
		ToolsetHash:  prev.ToolsetHash,
		ScopeKey:     prev.ScopeKey,
		Prompts:      prompts,
		PrepSettings: cloneAnyMap(prev.PrepSettings),
		Context:      cloneAnyMap(prev.Context),
		ToolCaps:     cloneAnyMap(prev.ToolCaps),
		Custom:       cloneAnyMap(prev.Custom),
		// Recompute based on identifying fields
		SourceHash: ComputeSourceHash(prev.ModelID, prev.BaseURL, prev.ToolsetHash, prev.ScopeKey),
		PrevSHA:    prevSHAHex,
	}

	return newBundle, nil
}
