package canonical

import "testing"

func TestEnvelopeAliasTable(t *testing.T) {
	table := NewAliasTable()
	canonicalID := EnvelopeID("ex_alias:response:0")
	key := AliasKey{
		Protocol: "openai.responses",
		Kind:     string(EnvResponse),
		NativeID: "resp_123",
		Index:    7,
	}

	if err := table.Remember(key, canonicalID); err != nil {
		t.Fatalf("Remember() error = %v", err)
	}
	got, ok := table.Resolve(key)
	if !ok {
		t.Fatal("Resolve() did not find alias")
	}
	if got != canonicalID {
		t.Fatalf("Resolve() = %q, want %q", got, canonicalID)
	}
	if err := table.Remember(key, EnvelopeID("ex_alias:response:1")); err == nil {
		t.Fatal("expected conflict when alias remaps to a different canonical id")
	}

	idx := NewEnvelopeIndex()
	nativeIndex := key.Index
	start := Event{
		ExchangeID: "ex_alias",
		Seq:        1,
		Kind:       EventEnvelopeStart,
		EnvID:      canonicalID,
		Payload:    EnvelopeStartPayload{Kind: EnvResponse},
		Meta: EventMetadataFields{
			Protocol:    key.Protocol,
			NativeID:    key.NativeID,
			NativeIndex: &nativeIndex,
		},
	}
	end := Event{
		ExchangeID: "ex_alias",
		Seq:        2,
		Kind:       EventEnvelopeEnd,
		EnvID:      canonicalID,
		Payload:    EnvelopeEndPayload{Kind: EnvResponse, Status: EnvelopeStatusCompleted},
	}
	if err := idx.Observe(start); err != nil {
		t.Fatalf("Observe(start) error = %v", err)
	}
	if err := idx.Observe(end); err != nil {
		t.Fatalf("Observe(end) error = %v", err)
	}
	resolved, ok := idx.ResolveAlias(key)
	if !ok {
		t.Fatal("ResolveAlias() did not find observed alias")
	}
	if resolved != canonicalID {
		t.Fatalf("ResolveAlias() = %q, want %q", resolved, canonicalID)
	}
	closed, ok := idx.Closed(canonicalID)
	if !ok {
		t.Fatal("Closed() did not find canonical envelope")
	}
	if closed.ID != canonicalID {
		t.Fatalf("Closed().ID = %q, want %q", closed.ID, canonicalID)
	}
}
