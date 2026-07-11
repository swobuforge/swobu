package profile

import (
	"testing"
)

func TestCatalog_SpecSupport(t *testing.T) {
	t.Parallel()

	if !SupportsSpec("openai") {
		t.Fatal("openai provider missing from catalog")
	}
	if !SupportsSpec("chatgpt") {
		t.Fatal("chatgpt provider missing from catalog")
	}
	if !SupportsSpec("anthropic") {
		t.Fatal("anthropic provider spec should be supported")
	}
	if !SupportsSpec("bedrock") {
		t.Fatal("bedrock provider spec should be supported")
	}
}

func TestCatalog_DefaultsAndCredentialPolicy(t *testing.T) {
	t.Parallel()

	if got := DefaultExecuteBaseURL("chatgpt"); got != "https://api.openai.com/v1" {
		t.Fatalf("chatgpt default base URL = %q", got)
	}
	if !RequiresCredential("chatgpt", DefaultExecuteBaseURL("chatgpt")) {
		t.Fatal("chatgpt should require credential")
	}
	if got := DefaultExecuteBaseURL("openrouter"); got != "https://openrouter.ai/api/v1" {
		t.Fatalf("openrouter default base URL = %q", got)
	}
	if !RequiresCredential("openrouter", DefaultExecuteBaseURL("openrouter")) {
		t.Fatal("openrouter should require credential")
	}
	if RequiresCredential("ollama", "http://127.0.0.1:11434/v1") {
		t.Fatal("ollama should not require credential")
	}
	if RequiresCredential("openai_compatible", "http://localhost:9999/v1") {
		t.Fatal("localhost OpenAI-compatible URL should not require credential")
	}
	if !RequiresCredential("openai_compatible", "https://lab.example/v1") {
		t.Fatal("remote OpenAI-compatible URL should require credential")
	}
	if RequiresCredential("bedrock", "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1") {
		t.Fatal("bedrock should default to AWS profile mode without credential_ref requirement")
	}

	chatgptVariants := SupportedAuthVariantsForSpec("chatgpt")
	if len(chatgptVariants) < 2 {
		t.Fatalf("chatgpt auth variants=%v want at least 2", chatgptVariants)
	}
	if chatgptVariants[0] != AuthVariantChatGPTLogin {
		t.Fatalf("chatgpt default auth variant=%q want=%q", chatgptVariants[0], AuthVariantChatGPTLogin)
	}

	modes := AllowedAuthModesForSpec("chatgpt")
	if len(modes) < 2 {
		t.Fatalf("chatgpt allowed auth modes=%v want at least 2", modes)
	}
	if modes[0].ID != AuthModeInteractiveBrowser || !modes[0].Interactive {
		t.Fatalf("chatgpt mode[0]=%+v", modes[0])
	}

	bedrockModes := AllowedAuthModesForSpec("bedrock")
	if len(bedrockModes) != 3 {
		t.Fatalf("bedrock allowed auth modes=%v want exactly 3", bedrockModes)
	}
	if bedrockModes[0].ID != AuthModeAWSProfile || bedrockModes[0].Variant != AuthVariantAWSProfile {
		t.Fatalf("bedrock mode[0]=%+v", bedrockModes[0])
	}
	if bedrockModes[1].ID != AuthModeAWSEnvSession || bedrockModes[1].Variant != AuthVariantAWSEnvSession {
		t.Fatalf("bedrock mode[1]=%+v", bedrockModes[1])
	}
	if bedrockModes[2].ID != AuthModeTokenEnv || bedrockModes[2].Variant != AuthVariantEnv {
		t.Fatalf("bedrock mode[2]=%+v", bedrockModes[2])
	}
}

func TestCatalog_ResolveRouteProfile(t *testing.T) {
	t.Parallel()

	profile, ok := ResolveRouteProfile("openai", "https://api.openai.com/v1", "cred-1")
	if !ok {
		t.Fatal("openai route profile should resolve")
	}
	if profile.AuthKind != AuthCredentialRef {
		t.Fatalf("auth kind = %q", profile.AuthKind)
	}

	if _, ok := ResolveRouteProfile("claude", "https://api.anthropic.com/v1", "cred-1"); ok {
		t.Fatal("claude provider spec must be rejected; use anthropic")
	}
}

func TestCatalog_ChatGPTProviderProtocols_AreStreamOnly(t *testing.T) {
	t.Parallel()

	protocols := SupportedProviderProtocolsForSpec("chatgpt")
	if len(protocols) == 0 {
		t.Fatal("chatgpt protocols must not be empty")
	}
	if SupportsProviderProtocolForSpec("chatgpt", "responses") {
		t.Fatal("chatgpt must not declare buffered responses protocol")
	}
	if !SupportsProviderProtocolForSpec("chatgpt", "responses_stream") {
		t.Fatal("chatgpt must declare responses_stream protocol")
	}
	if got, ok := ResolveConcreteProtocolForAutoAtBoundary("chatgpt"); !ok || got != "responses_stream" {
		t.Fatalf("chatgpt default protocol = %q (ok=%v), want responses_stream", got, ok)
	}
}

func TestCatalog_ConcreteProviderProtocolsForSpec_OrderIsCanonical(t *testing.T) {
	t.Parallel()

	openAI := ConcreteProviderProtocolsForSpec("openai")
	if len(openAI) < 2 {
		t.Fatalf("openai concrete protocols=%v want at least 2", openAI)
	}
	if openAI[0] != "responses" || openAI[1] != "responses_stream" {
		t.Fatalf("openai concrete protocol order=%v want [responses responses_stream ...]", openAI)
	}

	chatgpt := ConcreteProviderProtocolsForSpec("chatgpt")
	if len(chatgpt) != 1 || chatgpt[0] != "responses_stream" {
		t.Fatalf("chatgpt concrete protocols=%v want [responses_stream]", chatgpt)
	}
}
