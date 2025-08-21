package main

import (
<<<<<<< HEAD
    "github.com/hyperifyio/goagent/internal/oai"
=======
	"github.com/hyperifyio/goagent/internal/oai"
>>>>>>> cmd/agentcli: restore CLI behaviors and fix tests by reintroducing missing helpers and stubs
)

// applyTranscriptHygiene enforces transcript-size safeguards before requests.
// When debug is off, any role:"tool" message whose content exceeds 8 KiB is
// replaced with a compact JSON marker to prevent huge payloads from being sent
// upstream. Under -debug, no truncation occurs to preserve full visibility.
func applyTranscriptHygiene(in []oai.Message, debug bool) []oai.Message {
	if debug || len(in) == 0 {
		// Preserve exact transcript under -debug or when empty
		return in
	}
	const limit = 8 * 1024
	out := make([]oai.Message, 0, len(in))
	for _, m := range in {
		n := m
		if n.Role == oai.RoleTool {
			if len(n.Content) > limit {
				n.Content = `{"truncated":true,"reason":"large-tool-output"}`
			}
		}
		out = append(out, n)
	}
	return out
}
