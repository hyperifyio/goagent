package oai

import (
	"time"
)

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// LogLengthBackoff emits a structured NDJSON audit entry describing a
// length_backoff event triggered by finish_reason=="length". Callers should
// pass the model identifier, the previous and new completion caps, the
// effective model context window, and the estimated prompt token count.
func LogLengthBackoff(model string, prevCap, newCap, window, estimatedPromptTokens int) {
	type audit struct {
		TS                    string `json:"ts"`
		Event                 string `json:"event"`
		Model                 string `json:"model"`
		PrevCap               int    `json:"prev_cap"`
		NewCap                int    `json:"new_cap"`
		Window                int    `json:"window"`
		EstimatedPromptTokens int    `json:"estimated_prompt_tokens"`
	}
	entry := audit{
		TS:                    time.Now().UTC().Format(time.RFC3339Nano),
		Event:                 "length_backoff",
		Model:                 model,
		PrevCap:               prevCap,
		NewCap:                newCap,
		Window:                window,
		EstimatedPromptTokens: estimatedPromptTokens,
	}
	_ = appendAuditLog(entry)
}

// emitChatMetaAudit writes a one-line NDJSON entry describing request-level
// observability fields such as the effective temperature and whether the
// temperature parameter is included in the payload for the target model.
func emitChatMetaAudit(req ChatCompletionsRequest) {
	// Compute effective temperature based on model support and clamp rules.
	effectiveTemp, supported := EffectiveTemperatureForModel(req.Model, valueOrDefault(req.Temperature, 1.0))
	type meta struct {
		TS                   string  `json:"ts"`
		Event                string  `json:"event"`
		Model                string  `json:"model"`
		TemperatureEffective float64 `json:"temperature_effective"`
		TemperatureInPayload bool    `json:"temperature_in_payload"`
	}
	entry := meta{
		TS:                   time.Now().UTC().Format(time.RFC3339Nano),
		Event:                "chat_meta",
		Model:                req.Model,
		TemperatureEffective: effectiveTemp,
		TemperatureInPayload: supported && req.Temperature != nil,
	}
	_ = appendAuditLog(entry)
}

func valueOrDefault(ptr *float64, def float64) float64 {
	if ptr == nil {
		return def
	}
	return *ptr
}
