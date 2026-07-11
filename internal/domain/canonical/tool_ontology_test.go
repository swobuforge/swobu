package canonical

import "testing"

func TestToolPolicyClone_PreservesSpecificToolID(t *testing.T) {
	t.Parallel()

	id := NewSemanticToolID("tool_0")
	policy := NewToolPolicy(ToolPolicySpecific, &id)

	cloned := policy.Clone()
	specific, ok := cloned.SpecificID()
	if !ok {
		t.Fatal("cloned tool policy lost specific tool id")
	}
	if specific.String() != "tool_0" {
		t.Fatalf("specific tool id = %q, want %q", specific, "tool_0")
	}
	if cloned.Mode != ToolPolicySpecific {
		t.Fatalf("tool policy mode = %q, want %q", cloned.Mode, ToolPolicySpecific)
	}
}

func TestRequestToolDeclMetadata_RoundTripsCapabilityDeclarations(t *testing.T) {
	t.Parallel()

	decl := CapabilityToolDecl{
		ID:         NewSemanticToolID("cap_0"),
		Capability: NewToolCapability("web_search"),
		Config:     NewToolCapabilityConfigObject(`{"region":"us"}`),
		Execution:  ToolOwnerProvider,
	}

	raw, err := encodeRequestToolDeclsMetadata([]ToolDecl{decl})
	if err != nil {
		t.Fatalf("encodeRequestToolDeclsMetadata returned error: %v", err)
	}
	decoded, err := decodeRequestToolDeclsMetadata(raw)
	if err != nil {
		t.Fatalf("decodeRequestToolDeclsMetadata returned error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded len = %d, want 1", len(decoded))
	}
	got, ok := decoded[0].(CapabilityToolDecl)
	if !ok {
		t.Fatalf("decoded declaration = %T, want CapabilityToolDecl", decoded[0])
	}
	if got.ToolID() != decl.ToolID() {
		t.Fatalf("tool id = %q, want %q", got.ToolID(), decl.ToolID())
	}
	if got.ToolCapability() != decl.ToolCapability() {
		t.Fatalf("capability = %q, want %q", got.ToolCapability(), decl.ToolCapability())
	}
	if got.Owner() != ToolOwnerProvider {
		t.Fatalf("owner = %q, want %q", got.Owner(), ToolOwnerProvider)
	}
	if got.CapabilityConfig().RawObject() != `{"region":"us"}` {
		t.Fatalf("capability config = %q, want %q", got.CapabilityConfig().RawObject(), `{"region":"us"}`)
	}
}
