package thread

import "testing"

func TestDerivePreservesOnlyFramedEquality(t *testing.T) {
	first, err := Derive("client/x-opencode-session/v1", "workspace", "external")
	if err != nil {
		t.Fatal(err)
	}
	repeated, _ := Derive("client/x-opencode-session/v1", "workspace", "external")
	differentNamespace, _ := Derive("other", "workspace", "external")
	differentWorkspace, _ := Derive("client/x-opencode-session/v1", "other", "external")
	differentExternal, _ := Derive("client/x-opencode-session/v1", "workspace", "other")
	framedLeft, _ := Derive("test", "ab", "c")
	framedRight, _ := Derive("test", "a", "bc")
	if first != repeated {
		t.Fatal("equal derivation inputs produced different IDs")
	}
	for name, candidate := range map[string]ID{
		"namespace": differentNamespace,
		"workspace": differentWorkspace,
		"external":  differentExternal,
		"framing":   framedRight,
	} {
		if first == candidate || name == "framing" && framedLeft == candidate {
			t.Fatalf("%s did not distinguish derivation inputs", name)
		}
	}
}

func TestProjectIsStableScopedAndOpaque(t *testing.T) {
	id, _ := Derive("client/x-opencode-session/v1", "workspace", "raw-client-marker")
	first, err := Project("provider/opencode-session/v1", id)
	if err != nil {
		t.Fatal(err)
	}
	repeated, _ := Project("provider/opencode-session/v1", id)
	other, _ := Project("provider/other/v1", id)
	if first != repeated {
		t.Fatal("equal projection inputs produced different values")
	}
	if first == other {
		t.Fatal("projection namespace did not scope the output")
	}
	if first == "raw-client-marker" {
		t.Fatal("projection exposed the foreign identifier")
	}
}

func TestZeroAndEmptyNamespaceAreRejected(t *testing.T) {
	if !(ID{}).IsZero() {
		t.Fatal("zero ID was not detected")
	}
	if _, err := Derive(""); err == nil {
		t.Fatal("empty derivation namespace was accepted")
	}
	if _, err := Project("provider", ID{}); err == nil {
		t.Fatal("zero ID projection was accepted")
	}
	if id, _ := Derive("test", "value"); id.IsZero() {
		t.Fatal("derived ID is zero")
	}
}
