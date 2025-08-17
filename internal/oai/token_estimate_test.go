package oai

import "testing"

func TestEstimateTokens_MonotonicGrowth(t *testing.T) {
	msgs := []Message{{Role: RoleUser, Content: "hi"}}
	t1 := EstimateTokens(msgs)
	if t1 <= 0 {
		t.Fatalf("expected positive estimate, got %d", t1)
	}

	msgs = append(msgs, Message{Role: RoleAssistant, Content: "hello there"})
	t2 := EstimateTokens(msgs)
	if t2 <= t1 {
		t.Fatalf("expected estimate to grow, got t1=%d t2=%d", t1, t2)
	}

	msgs = append(msgs, Message{Role: RoleTool, ToolCallID: "call_1", Content: "{\"ok\":true}"})
	t3 := EstimateTokens(msgs)
	if t3 <= t2 {
		t.Fatalf("expected estimate to grow with tool call, got t2=%d t3=%d", t2, t3)
	}
}

func TestEstimateTokens_RoughScale(t *testing.T) {
	// 400 characters should be roughly ~100 tokens (+ overhead)
	content := make([]byte, 400)
	for i := range content {
		content[i] = 'a'
	}
	msgs := []Message{{Role: RoleUser, Content: string(content)}}
	est := EstimateTokens(msgs)
	if est < 90 || est > 130 { // allow a generous band
		t.Fatalf("expected estimate around 100±30, got %d", est)
	}
}
