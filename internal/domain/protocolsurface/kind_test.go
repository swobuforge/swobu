package protocolsurface

import "testing"

func TestParse_RoundTripsKnownKinds(t *testing.T) {
	for _, raw := range []string{"chat_completions", "responses", "completions", "messages"} {
		kind, err := Parse(raw)
		if err != nil {
			t.Fatalf("Parse(%q) returned error: %v", raw, err)
		}
		if got := kind.String(); got != raw {
			t.Fatalf("String() = %q, want %q", got, raw)
		}
		text, err := kind.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%q) returned error: %v", raw, err)
		}
		var decoded Kind
		if err := decoded.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q) returned error: %v", raw, err)
		}
		if decoded != kind {
			t.Fatalf("decoded kind = %q, want %q", decoded, kind)
		}
	}
}

func TestParse_RejectsUnknownKind(t *testing.T) {
	if _, err := Parse("openai_compat"); err == nil {
		t.Fatal("expected error, got nil")
	}
}
