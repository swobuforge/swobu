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

func TestProtocolKindRejectsUnknownWireIdentity(t *testing.T) {
	if _, err := ParseProtocolKind("generate_content"); err == nil {
		t.Fatal("unknown provider wire identity was accepted")
	}
}
