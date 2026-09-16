package protocolkind

import "testing"

func TestInteractionsIsAClosedProtocolKind(t *testing.T) {
	parsed, err := ParseProtocolKind("interactions")
	if err != nil {
		t.Fatalf("ParseProtocolKind(interactions): %v", err)
	}
	if parsed != Interactions {
		t.Fatalf("parsed = %q, want %q", parsed, Interactions)
	}
	encoded, err := Interactions.MarshalText()
	if err != nil || string(encoded) != "interactions" {
		t.Fatalf("Interactions.MarshalText() = %q, %v", encoded, err)
	}
}

func TestGenerateContentIsAClosedClientIngressProtocolKind(t *testing.T) {
	parsed, err := ParseProtocolKind("generate_content")
	if err != nil {
		t.Fatalf("ParseProtocolKind(generate_content): %v", err)
	}
	if parsed != GenerateContent {
		t.Fatalf("parsed = %q, want %q", parsed, GenerateContent)
	}
}
