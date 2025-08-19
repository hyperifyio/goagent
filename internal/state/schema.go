package state

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// StateBundle is the versioned persisted execution state snapshot.
// Only JSON-serializable fields; do not include runtime-only data.
// Version must be "1" for the initial schema.
// All timestamps are RFC3339 with UTC timezone.
// Files must be written with permissions 0600.
// The pointer file latest.json must contain a JSON object pointing to a concrete snapshot path.

type StateBundle struct {
	Version      string            `json:"version"`
	CreatedAt    string            `json:"created_at"`
	ToolVersion  string            `json:"tool_version"`
	ModelID      string            `json:"model_id"`
	BaseURL      string            `json:"base_url"`
	ToolsetHash  string            `json:"toolset_hash"`
	ScopeKey     string            `json:"scope_key"`
	Prompts      map[string]string `json:"prompts"`
	PrepSettings map[string]any    `json:"prep_settings"`
	Context      map[string]any    `json:"context"`
	ToolCaps     map[string]any    `json:"tool_caps"`
	Custom       map[string]any    `json:"custom"`
	SourceHash   string            `json:"source_hash"`
	PrevSHA      string            `json:"prev_sha,omitempty"`
}

var (
	errInvalidVersion   = errors.New("invalid version")
	errMissingTimestamp = errors.New("invalid created_at")
	errMissingModel     = errors.New("missing model_id")
	errMissingBaseURL   = errors.New("missing base_url")
	errMissingScope     = errors.New("missing scope_key")
)

// Validate returns nil if the bundle is structurally valid for version 1.
func (b *StateBundle) Validate() error {
	if b == nil {
		return errors.New("nil bundle")
	}
	if b.Version != "1" {
		return fmt.Errorf("%w: %s", errInvalidVersion, b.Version)
	}
	if _, err := time.Parse(time.RFC3339, b.CreatedAt); err != nil {
		return errMissingTimestamp
	}
	if b.ModelID == "" {
		return errMissingModel
	}
	if b.BaseURL == "" {
		return errMissingBaseURL
	}
	if b.ScopeKey == "" {
		return errMissingScope
	}
	// Optional maps may be nil; normalize callers should handle nil.
	return nil
}

// ComputeSourceHash returns a hex-encoded SHA-256 of select identifying fields.
// This is used to detect changes across runs; callers decide the exact input.
func ComputeSourceHash(modelID string, baseURL string, toolsetHash string, scopeKey string) string {
	input := modelID + "|" + baseURL + "|" + toolsetHash + "|" + scopeKey
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}
