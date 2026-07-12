package canonical

import "testing"

func TestToolCallBatchPolicy_MetadataRoundTrips(t *testing.T) {
	policy := NewToolCallBatchPolicy(ToolCallBatchAtMostOne)
	if err := policy.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	raw, err := encodeToolCallBatchMetadata(policy)
	if err != nil {
		t.Fatalf("encodeToolCallBatchMetadata returned error: %v", err)
	}
	decoded, err := decodeToolCallBatchMetadata(raw)
	if err != nil {
		t.Fatalf("decodeToolCallBatchMetadata returned error: %v", err)
	}
	if decoded.Mode != ToolCallBatchAtMostOne {
		t.Fatalf("decoded mode = %q, want %q", decoded.Mode, ToolCallBatchAtMostOne)
	}
}

func TestToolCallBatchPolicy_ZeroValueIsInert(t *testing.T) {
	var policy ToolCallBatchPolicy
	if !policy.IsZero() {
		t.Fatal("zero-value policy must be inert")
	}
	raw, err := encodeToolCallBatchMetadata(policy)
	if err != nil {
		t.Fatalf("encodeToolCallBatchMetadata returned error: %v", err)
	}
	if raw != "" {
		t.Fatalf("encodeToolCallBatchMetadata = %q, want empty", raw)
	}
}
