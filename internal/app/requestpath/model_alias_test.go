package requestpath

import "testing"

func TestCanonicalModelAlias_FormatsProviderAndModel(t *testing.T) {
	got := CanonicalModelAlias(" openai ", " gpt-4.1 ")
	if got != "openai:gpt-4.1" {
		t.Fatalf("alias = %q, want %q", got, "openai:gpt-4.1")
	}
}

func TestCanonicalModelAlias_PanicsWhenProviderSpecEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty providerSpec")
		}
	}()
	_ = CanonicalModelAlias("", "gpt-4.1")
}

func TestCanonicalModelAlias_PanicsWhenBackendModelIDEmpty(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty backendModelID")
		}
	}()
	_ = CanonicalModelAlias("openai", "  ")
}
