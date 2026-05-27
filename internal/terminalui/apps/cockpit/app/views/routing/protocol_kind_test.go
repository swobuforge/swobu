package routing

import "testing"

func TestDefaultProviderProtocolForProvider(t *testing.T) {
	t.Parallel()

	if got := defaultProviderProtocolForProvider("anthropic"); got != "auto" {
		t.Fatalf("anthropic default protocol=%q want auto", got)
	}
	if got := defaultProviderProtocolForProvider("openrouter"); got != "auto" {
		t.Fatalf("openrouter default protocol=%q want auto", got)
	}
	if got := defaultProviderProtocolForProvider(""); got != "auto" {
		t.Fatalf("fallback default protocol=%q want auto", got)
	}
}
