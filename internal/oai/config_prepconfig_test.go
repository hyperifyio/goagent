package oai

import "testing"

func TestNewPrepConfig_SetsPrompt(t *testing.T) {
    cfg := NewPrepConfig([]string{"one", "two"})
    if cfg.Prompt != "one\n\ntwo" {
        t.Fatalf("unexpected prompt: %q", cfg.Prompt)
    }
}
