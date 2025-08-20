package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/hyperifyio/goagent/internal/oai"
)

// printResolvedConfig writes a JSON object describing resolved configuration and returns exit code 0.
func printResolvedConfig(cfg cliConfig, stdout io.Writer) int {
	// Ensure timeouts are normalized as in runAgent
	if cfg.httpTimeout <= 0 {
		if cfg.timeout > 0 {
			cfg.httpTimeout = cfg.timeout
		} else {
			cfg.httpTimeout = 90 * time.Second
		}
	}
	if cfg.toolTimeout <= 0 {
		if cfg.timeout > 0 {
			cfg.toolTimeout = cfg.timeout
		} else {
			cfg.toolTimeout = 30 * time.Second
		}
	}
	// Default sources when unset
	if strings.TrimSpace(cfg.httpTimeoutSource) == "" {
		cfg.httpTimeoutSource = "default"
	}
	if strings.TrimSpace(cfg.prepHTTPTimeoutSource) == "" {
		cfg.prepHTTPTimeoutSource = "inherit"
	}
	if strings.TrimSpace(cfg.toolTimeoutSource) == "" {
		cfg.toolTimeoutSource = "default"
	}
	if strings.TrimSpace(cfg.globalTimeoutSource) == "" {
		cfg.globalTimeoutSource = "default"
	}

	// Build a minimal, stable JSON payload
	payload := map[string]any{
		"model":                 cfg.model,
		"baseURL":               cfg.baseURL,
		"httpTimeout":           cfg.httpTimeout.String(),
		"httpTimeoutSource":     cfg.httpTimeoutSource,
		"prepHTTPTimeout":       cfg.prepHTTPTimeout.String(),
		"prepHTTPTimeoutSource": cfg.prepHTTPTimeoutSource,
		"toolTimeout":           cfg.toolTimeout.String(),
		"toolTimeoutSource":     cfg.toolTimeoutSource,
		"timeout":               cfg.timeout.String(),
		"timeoutSource":         cfg.globalTimeoutSource,
	}

	// Resolve prep-specific view for printing
	prepModel, prepModelSource := cfg.prepModel, cfg.prepModelSource
	prepBase, prepBaseSource := cfg.prepBaseURL, cfg.prepBaseURLSource
	var apiKeyPresent bool
	apiKeySource := cfg.prepAPIKeySource
	if strings.TrimSpace(cfg.prepAPIKey) != "" {
		apiKeyPresent = true
	} else {
		apiKeyPresent = false
	}

	// Resolve sampling for prep: one-knob behavior with explicit overrides
	var prepTempStr, prepTempSource, prepTopPStr, prepTopPSource string
	if cfg.prepTopP > 0 {
		prepTopPStr = strconv.FormatFloat(cfg.prepTopP, 'f', -1, 64)
		prepTopPSource = cfg.prepTopPSource
		prepTempStr = "(omitted)"
		prepTempSource = "omitted:one-knob"
	} else if cfg.prepTemperatureSource == "flag" || cfg.prepTemperatureSource == "env" {
		if oai.SupportsTemperature(prepModel) {
			prepTempStr = strconv.FormatFloat(cfg.prepTemperature, 'f', -1, 64)
			prepTempSource = cfg.prepTemperatureSource
			prepTopPStr = "(omitted)"
			prepTopPSource = "inherit"
		} else {
			prepTempStr = "(omitted:unsupported)"
			prepTempSource = "unsupported"
			prepTopPStr = "(omitted)"
			prepTopPSource = "inherit"
		}
	} else {
		if oai.SupportsTemperature(prepModel) {
			prepTempStr = strconv.FormatFloat(cfg.temperature, 'f', -1, 64)
			prepTempSource = cfg.temperatureSource
			prepTopPStr = "(omitted)"
			prepTopPSource = "inherit"
		} else {
			prepTempStr = "(omitted:unsupported)"
			prepTempSource = "unsupported"
			prepTopPStr = "(omitted)"
			prepTopPSource = "inherit"
		}
	}

	// Pre-stage block
	payload["prep"] = map[string]any{
		"enabled":                cfg.prepEnabled,
		"model":                  prepModel,
		"modelSource":            prepModelSource,
		"baseURL":                prepBase,
		"baseURLSource":          prepBaseSource,
		"apiKeyPresent":          apiKeyPresent,
		"apiKeySource":           apiKeySource,
		"httpTimeout":            cfg.prepHTTPTimeout.String(),
		"httpTimeoutSource":      cfg.prepHTTPTimeoutSource,
		"httpRetries":            cfg.prepHTTPRetries,
		"httpRetriesSource":      cfg.prepHTTPRetriesSource,
		"httpRetryBackoff":       cfg.prepHTTPBackoff.String(),
		"httpRetryBackoffSource": cfg.prepHTTPBackoffSource,
		"sampling": map[string]any{
			"temperature":       prepTempStr,
			"temperatureSource": prepTempSource,
			"top_p":             prepTopPStr,
			"top_pSource":       prepTopPSource,
		},
	}
	// Image block with redacted API key
	{
		img, baseSrc, keySrc := oai.ResolveImageConfig(cfg.imageBaseURL, cfg.imageAPIKey, cfg.baseURL, cfg.apiKey)
		payload["image"] = map[string]any{
			"baseURL":                img.BaseURL,
			"baseURLSource":          baseSrc,
			"apiKey":                 oai.MaskAPIKeyLast4(img.APIKey),
			"apiKeySource":           keySrc,
			"model":                  cfg.imageModel,
			"httpTimeout":            cfg.imageHTTPTimeout.String(),
			"httpTimeoutSource":      nonEmptyOr(cfg.imageHTTPTimeoutSource, "inherit"),
			"httpRetries":            cfg.imageHTTPRetries,
			"httpRetriesSource":      nonEmptyOr(cfg.imageHTTPRetriesSource, "inherit"),
			"httpRetryBackoff":       cfg.imageHTTPBackoff.String(),
			"httpRetryBackoffSource": nonEmptyOr(cfg.imageHTTPBackoffSource, "inherit"),
			"n":                      cfg.imageN,
			"size":                   cfg.imageSize,
			"quality":                cfg.imageQuality,
			"style":                  cfg.imageStyle,
			"response_format":        cfg.imageResponseFormat,
			"transparent_background": cfg.imageTransparentBackground,
		}
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		// Fallback to a simple line to avoid surprising exits
		safeFprintln(stdout, "{}")
		return 0
	}
	safeFprintln(stdout, string(data))
	return 0
}
