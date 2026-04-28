package runtimeevidence

import "testing"

func TestParseRequestID_RejectsEmpty(t *testing.T) {
	if _, err := ParseRequestID("   "); err == nil {
		t.Fatal("ParseRequestID should reject empty input")
	}
}

func TestParseRequestID_PreservesStableValue(t *testing.T) {
	id, err := ParseRequestID("req-123")
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	if got := id.String(); got != "req-123" {
		t.Fatalf("request id = %q, want %q", got, "req-123")
	}
}
