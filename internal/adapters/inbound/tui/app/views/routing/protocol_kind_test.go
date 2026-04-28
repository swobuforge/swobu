package routing

import "testing"

func TestDefaultProtocolKindForProvider(t *testing.T) {
	t.Parallel()

	if got := defaultProtocolKindForProvider("anthropic"); got != "messages" {
		t.Fatalf("anthropic protocol kind = %q, want %q", got, "messages")
	}
	if got := defaultProtocolKindForProvider("openrouter"); got != "chat_completions" {
		t.Fatalf("openrouter protocol kind = %q, want %q", got, "chat_completions")
	}
	if got := defaultProtocolKindForProvider(""); got != "chat_completions" {
		t.Fatalf("empty provider protocol kind = %q, want %q", got, "chat_completions")
	}
}
