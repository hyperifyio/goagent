package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hyperifyio/goagent/internal/oai"
)

// parseSavedMessages accepts either a JSON array of oai.Message (legacy format)
// or a JSON object {"messages":[...], "image_prompt":"..."} and returns
// the parsed messages and optional image prompt.
func parseSavedMessages(data []byte) ([]oai.Message, string, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		var msgs []oai.Message
		if err := json.Unmarshal([]byte(trimmed), &msgs); err != nil {
			return nil, "", err
		}
		return msgs, "", nil
	}
	var wrapper struct {
		Messages    []oai.Message `json:"messages"`
		ImagePrompt string        `json:"image_prompt"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapper); err != nil {
		return nil, "", err
	}
	return wrapper.Messages, strings.TrimSpace(wrapper.ImagePrompt), nil
}

// buildMessagesWrapper constructs the saved/printed JSON wrapper including
// the Harmony messages, optional image prompt, and pre-stage metadata.
func buildMessagesWrapper(messages []oai.Message, imagePrompt string) any {
	// Determine pre-stage prompt source and size using resolver.
	// Flags for pre-stage prompt are not yet implemented; this will resolve to
	// the embedded default for now, which is acceptable and deterministic.
	src, text := oai.ResolvePrepPrompt(nil, "")
	type prestageMeta struct {
		Source string `json:"source"`
		Bytes  int    `json:"bytes"`
	}
	type wrapper struct {
		Messages    []oai.Message `json:"messages"`
		ImagePrompt string        `json:"image_prompt,omitempty"`
		Prestage    prestageMeta  `json:"prestage"`
	}
	w := wrapper{
		Messages: messages,
		Prestage: prestageMeta{Source: src, Bytes: len([]byte(text))},
	}
	if strings.TrimSpace(imagePrompt) != "" {
		w.ImagePrompt = strings.TrimSpace(imagePrompt)
	}
	return w
}

// writeSavedMessages writes the wrapper JSON with messages, optional image_prompt,
// and pre-stage metadata.
func writeSavedMessages(path string, messages []oai.Message, imagePrompt string) error {
	wrapper := buildMessagesWrapper(messages, strings.TrimSpace(imagePrompt))
	b, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b, 0o644)
}
