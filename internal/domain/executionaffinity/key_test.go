package executionaffinity

import "testing"

func TestDeriveAndProjectAreStableAndOpaque(t *testing.T) {
	a, err := Derive("client/x-opencode-session/v1", "alpha", "session")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Derive("client/x-opencode-session/v1", "alpha", "session")
	if err != nil {
		t.Fatal(err)
	}
	if a != b || a.IsZero() {
		t.Fatal("equal derivations must produce one non-zero key")
	}
	projected, err := Project("provider/opencode-session/v1", a)
	if err != nil || projected == "" {
		t.Fatalf("project = %q, %v", projected, err)
	}
}
