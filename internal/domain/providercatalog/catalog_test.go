package providercatalog

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
)

func TestCatalog_AdapterAndBindingSupport(t *testing.T) {
	t.Parallel()

	adapter, ok := AdapterForSpec("openai")
	if !ok {
		t.Fatal("openai provider missing from catalog")
	}
	if adapter != AdapterCustomOpenAICompatible {
		t.Fatalf("openai adapter = %q", adapter)
	}
	if !SupportsSpec("anthropic") {
		t.Fatal("anthropic provider spec should be supported")
	}
	if !SupportsRoute("anthropic", protocolsurface.Messages) {
		t.Fatal("anthropic+messages should be supported")
	}
	for _, kind := range []protocolsurface.Kind{
		protocolsurface.ChatCompletions,
		protocolsurface.Responses,
		protocolsurface.Completions,
	} {
		if SupportsRoute("anthropic", kind) {
			t.Fatalf("anthropic+%s must not be declared supported until executor support exists", kind)
		}
	}
}

func TestCatalog_DefaultsAndCredentialPolicy(t *testing.T) {
	t.Parallel()

	if got := DefaultBaseURL("chatgpt"); got != "https://api.openai.com/v1" {
		t.Fatalf("chatgpt default base URL = %q", got)
	}
	if !RequiresCredential("chatgpt", DefaultBaseURL("chatgpt")) {
		t.Fatal("chatgpt should require credential")
	}
	if got := DefaultBaseURL("openrouter"); got != "https://openrouter.ai/api/v1" {
		t.Fatalf("openrouter default base URL = %q", got)
	}
	if !RequiresCredential("openrouter", DefaultBaseURL("openrouter")) {
		t.Fatal("openrouter should require credential")
	}
	if RequiresCredential("ollama", "http://127.0.0.1:11434/v1") {
		t.Fatal("ollama should not require credential")
	}
	if RequiresCredential("custom", "http://localhost:9999/v1") {
		t.Fatal("localhost custom URL should not require credential")
	}
	if !RequiresCredential("custom", "https://lab.example/v1") {
		t.Fatal("remote custom URL should require credential")
	}

	protocol, ok := DefaultProtocolForSpec("anthropic")
	if !ok {
		t.Fatal("anthropic default protocol missing")
	}
	if protocol != protocolsurface.Messages {
		t.Fatalf("anthropic default protocol = %q, want %q", protocol, protocolsurface.Messages)
	}

	protocol, ok = DefaultProtocolForSpec("openrouter")
	if !ok {
		t.Fatal("openrouter default protocol missing")
	}
	if protocol != protocolsurface.ChatCompletions {
		t.Fatalf("openrouter default protocol = %q, want %q", protocol, protocolsurface.ChatCompletions)
	}

	chatgptVariants := SupportedAuthVariantsForSpec("chatgpt")
	if len(chatgptVariants) < 2 {
		t.Fatalf("chatgpt auth variants=%v want at least 2", chatgptVariants)
	}
	if chatgptVariants[0] != AuthVariantChatGPTLogin {
		t.Fatalf("chatgpt default auth variant=%q want=%q", chatgptVariants[0], AuthVariantChatGPTLogin)
	}
}

func TestCatalog_ResolveRouteProfile(t *testing.T) {
	t.Parallel()

	profile, ok := ResolveRouteProfile("openai", protocolsurface.ChatCompletions, "https://api.openai.com/v1", "cred-1")
	if !ok {
		t.Fatal("openai route profile should resolve")
	}
	if profile.ExecutionAdapterID != AdapterCustomOpenAICompatible {
		t.Fatalf("adapter = %q", profile.ExecutionAdapterID)
	}
	if profile.AuthKind != AuthCredentialRef {
		t.Fatalf("auth kind = %q", profile.AuthKind)
	}
	if profile.APIFamily != EndpointModeOpenAICompatible {
		t.Fatalf("endpoint mode = %q", profile.APIFamily)
	}

	if _, ok := ResolveRouteProfile("custom", protocolsurface.Messages, "https://example.test/v1", "cred-1"); ok {
		t.Fatal("custom+messages must be rejected")
	}
}
