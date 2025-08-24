package oai

import "testing"

// helper to build a message with role and content
func m(role, content string) Message { return Message{Role: role, Content: content} }

func TestTrimMessagesToFit_PreservesSystemAndDeveloper(t *testing.T) {
    sys := m(RoleSystem, repeat("S", 4000))      // ~1000 tokens
    dev := m(RoleDeveloper, repeat("D", 4000))   // ~1000 tokens
    u1 := m(RoleUser, repeat("u", 4000))        // ~1000 tokens
    a1 := m(RoleAssistant, repeat("a", 4000))   // ~1000 tokens
    u2 := m(RoleUser, repeat("u", 4000))        // ~1000 tokens
    in := []Message{sys, dev, u1, a1, u2}

    // Limit so that we cannot keep all messages; must drop from the front (u1,a1)
    limit := EstimateTokens(in) - 1500
    out := TrimMessagesToFit(in, limit)

    if EstimateTokens(out) > limit {
        t.Fatalf("trim did not reduce to limit: got=%d limit=%d", EstimateTokens(out), limit)
    }
    if len(out) >= 2 {
        if out[0].Role != RoleSystem {
            t.Fatalf("first message should be system; got %q", out[0].Role)
        }
        if out[1].Role != RoleDeveloper {
            t.Fatalf("second message should be developer; got %q", out[1].Role)
        }
    } else {
        t.Fatalf("expected to preserve at least system and developer; got %d", len(out))
    }
}

func TestTrimMessagesToFit_DropsOldestNonPinned(t *testing.T) {
    sys := m(RoleSystem, "policy")
    // 5 alternating user/assistant messages
    msgs := []Message{sys}
    for i := 0; i < 5; i++ {
        msgs = append(msgs, m(RoleUser, repeat("U", 2000)))
        msgs = append(msgs, m(RoleAssistant, repeat("A", 2000)))
    }
    // Force heavy trim
    limit := EstimateTokens(msgs) / 2
    out := TrimMessagesToFit(msgs, limit)
    if EstimateTokens(out) > limit {
        t.Fatalf("expected tokens <= limit; got=%d limit=%d", EstimateTokens(out), limit)
    }
    // Ensure the newest non-pinned message remains (the last assistant)
    if out[len(out)-1].Role != RoleAssistant {
        t.Fatalf("expected newest assistant at tail; got %q", out[len(out)-1].Role)
    }
}

func TestTrimMessagesToFit_OnlySystemDeveloperTooLarge_TruncatesContent(t *testing.T) {
    sys := m(RoleSystem, repeat("S", 20000))    // ~5000 tokens
    dev := m(RoleDeveloper, repeat("D", 20000)) // ~5000 tokens
    in := []Message{sys, dev}
    limit := 3000 // far below combined estimate
    out := TrimMessagesToFit(in, limit)
    if EstimateTokens(out) > limit {
        t.Fatalf("expected tokens <= limit after truncation; got=%d limit=%d", EstimateTokens(out), limit)
    }
    if len(out) != 2 {
        t.Fatalf("should keep both system and developer; got %d", len(out))
    }
    if len(out[0].Content) >= len(sys.Content) {
        t.Fatalf("system content was not truncated")
    }
    if len(out[1].Content) >= len(dev.Content) {
        t.Fatalf("developer content was not truncated")
    }
}

// repeat returns a string consisting of count repetitions of s.
func repeat(s string, count int) string {
    if count <= 0 {
        return ""
    }
    b := make([]byte, 0, len(s)*count)
    for i := 0; i < count; i++ {
        b = append(b, s...)
    }
    return string(b)
}
