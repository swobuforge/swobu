package canonical

import "testing"

func TestToolSetPreservesDeclarationOrderAndRejectsDuplicates(t *testing.T) {
	alpha := testFunctionTool(testRequestToolKey(ToolKindFunction, "alpha"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	zeta := testFunctionTool(testRequestToolKey(ToolKindFunction, "zeta"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	set, err := NewToolSet([]ToolDeclaration{zeta, alpha})
	if err != nil {
		t.Fatalf("NewToolSet: %v", err)
	}
	declarations := set.Declarations()
	if got := declarations[0].Key().String(); got != zeta.Key().String() {
		t.Fatalf("first tool key = %q, want %q", got, zeta.Key().String())
	}
	if got := declarations[1].Key().String(); got != alpha.Key().String() {
		t.Fatalf("second tool key = %q, want %q", got, alpha.Key().String())
	}
	if _, err := NewToolSet([]ToolDeclaration{alpha, alpha.Clone()}); err == nil {
		t.Fatal("NewToolSet accepted duplicate ToolKey")
	}
}

func TestToolSetLookupReturnsClone(t *testing.T) {
	declaration := testFunctionTool(testRequestToolKey(ToolKindFunction, "lookup"), "", testToolSchema(`{"type":"object"}`), Unspecified[bool]())
	set, err := NewToolSet([]ToolDeclaration{declaration})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := set.Lookup(declaration.Key())
	if !ok || got.Key().String() != declaration.Key().String() {
		t.Fatalf("Lookup = (%v, %v)", got, ok)
	}
}

func TestSpecifiedDistinguishesOmittedAndExplicitEmpty(t *testing.T) {
	if Unspecified[string]().IsSpecified() {
		t.Fatal("Unspecified reports supplied")
	}
	explicit := Specify("")
	value, ok := explicit.Get()
	if !ok || value != "" {
		t.Fatalf("Specify(empty).Get = (%q, %v)", value, ok)
	}
}
