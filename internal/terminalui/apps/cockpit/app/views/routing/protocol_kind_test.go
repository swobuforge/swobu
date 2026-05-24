package routing

import "testing"

func TestDefaultProviderProtocolForProvider(t *testing.T) {
	t.Parallel()

	if got := defaultProviderProtocolForProvider("anthropic"); got == "" {
		t.Fatal("anthropic provider protocol should not be empty")
	}
	if got := defaultProviderProtocolForProvider("openrouter"); got == "" {
		t.Fatal("openrouter provider protocol should not be empty")
	}
	if got := defaultProviderProtocolForProvider(""); got == "" {
		t.Fatal("default provider protocol should not be empty")
	}
}
