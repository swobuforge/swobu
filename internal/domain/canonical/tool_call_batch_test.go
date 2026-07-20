package canonical

import "testing"

func TestToolCallBatchPolicyValidatesAtMostOne(t *testing.T) {
	policy := NewToolCallBatchPolicy(ToolCallBatchAtMostOne)
	if err := policy.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if clone := policy.Clone(); clone.Mode != ToolCallBatchAtMostOne {
		t.Fatalf("clone mode = %q, want %q", clone.Mode, ToolCallBatchAtMostOne)
	}
}

func TestToolCallBatchPolicy_ZeroValueIsInert(t *testing.T) {
	var policy ToolCallBatchPolicy
	if !policy.IsZero() {
		t.Fatal("zero-value policy must be inert")
	}
}
