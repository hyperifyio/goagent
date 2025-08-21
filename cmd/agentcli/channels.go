package main

import "strings"

// resolveChannelRoute returns the destination for a given assistant channel.
// Defaults: final→stdout; non-final (critic/confidence)→stderr. Unknown/empty
// channels default to final behavior. When an override is provided via
// -channel-route, it takes precedence.
func resolveChannelRoute(cfg cliConfig, channel string, nonFinal bool) string {
	ch := strings.TrimSpace(channel)
	if ch == "" {
		ch = "final"
	}
	if cfg.channelRoutes != nil {
		if dest, ok := cfg.channelRoutes[ch]; ok {
			return dest
		}
	}
	if ch == "final" {
		return "stdout"
	}
	// Default non-final route
	return "stderr"
}
