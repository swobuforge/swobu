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
	if !SupportsSpec("azure") {
		t.Fatal("azure provider spec should be supported")
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
	if !RequiresExplicitEndpoint("azure") {
		t.Fatal("azure should require an explicit endpoint")
	}
	if !RequiresExplicitEndpoint("bedrock") {
		t.Fatal("bedrock should require an explicit endpoint")
	}
	if got := DefaultEnvKeyForSpec("azure"); got != "AZURE_OPENAI_API_KEY" {
		t.Fatalf("azure default env key = %q", got)
	}
	if got := DefaultEnvKeyForSpec("bedrock"); got != "AWS_BEARER_TOKEN_BEDROCK" {
		t.Fatalf("bedrock default env key = %q", got)
	}
	if got := DefaultAuthHeaderForSpec("openai_compatible"); got != "Authorization" {
		t.Fatalf("openai-compatible default auth header = %q", got)
	}
	authHeaders := SupportedAuthHeadersForSpec("openai_compatible")
	if len(authHeaders) != 3 {
		t.Fatalf("openai-compatible auth headers=%v want 3 common choices", authHeaders)
	}
	if authHeaders[0] != "Authorization" || authHeaders[1] != "x-api-key" || authHeaders[2] != "api-key" {
		t.Fatalf("openai-compatible auth headers=%v want [Authorization x-api-key api-key]", authHeaders)
	}
	for _, tc := range []struct {
		baseURL string
		want    string
	}{
		{"https://lab.example/v1", "Authorization"},
		{"", "Authorization"},
		{"https://gw.example/anthropic/v1/messages", "x-api-key"},
		{"https://foo.openai.azure.com/openai/deployments/x", "api-key"},
		{"https://foo.cognitiveservices.azure.com/openai", "api-key"},
		{"https://foo.services.ai.azure.com/anthropic/v1/messages", "x-api-key"},
	} {
		if got := InferredCredentialHeaderForBackendURL(tc.baseURL); got != tc.want {
			t.Fatalf("inferred credential header for %q = %q, want %q", tc.baseURL, got, tc.want)
		}
	}
	if !RequiresCredential("azure", "") {
		t.Fatal("azure should require credential")
	}
	if RequiresCredential("bedrock", "https://bedrock-mantle.us-east-1.api.aws/v1") {
		t.Fatal("bedrock profile layer should allow ambient AWS auth without credential_ref")
	}

	chatgptModes := SupportedAuthModesForSpec("chatgpt")
	if len(chatgptModes) < 2 {
		t.Fatalf("chatgpt auth modes=%v want at least 2", chatgptModes)
	}
	if chatgptModes[0] != AuthModeChatGPTLogin {
		t.Fatalf("chatgpt first listed auth mode=%q want=%q", chatgptModes[0], AuthModeChatGPTLogin)
	}

	openAIModes := SupportedAuthModesForSpec("openai")
	if len(openAIModes) != 3 {
		t.Fatalf("openai auth modes=%v want exactly 3", openAIModes)
	}
	if openAIModes[0] != AuthModeEnv || openAIModes[1] != AuthModeFile || openAIModes[2] != AuthModeKeychain {
		t.Fatalf("openai auth modes=%v want env/file/keychain", openAIModes)
	}

	modes := AllowedAuthModesForSpec("chatgpt")
	if len(modes) < 2 {
		t.Fatalf("chatgpt allowed auth modes=%v want at least 2", modes)
	}
	if modes[0].Mode != AuthModeChatGPTLogin || !modes[0].Interactive {
		t.Fatalf("chatgpt mode[0]=%+v", modes[0])
	}

	bedrockModes := AllowedAuthModesForSpec("bedrock")
	if len(bedrockModes) != 3 {
		t.Fatalf("bedrock allowed auth modes=%v want exactly 3", bedrockModes)
	}
	if bedrockModes[0].Mode != AuthModeEnv || bedrockModes[0].Label != "Bedrock API key" {
		t.Fatalf("bedrock mode[0]=%+v", bedrockModes[0])
	}
	if bedrockModes[1].Mode != AuthModeAWSEnvSession || bedrockModes[1].Label != "AWS env" {
		t.Fatalf("bedrock mode[1]=%+v", bedrockModes[1])
	}
	if bedrockModes[2].Mode != AuthModeAWSProfile || bedrockModes[2].Label != "AWS profile" {
		t.Fatalf("bedrock mode[2]=%+v", bedrockModes[2])
	}

	bedrockProtocols := ConcreteProviderProtocolsForSpec("bedrock")
	if len(bedrockProtocols) != 6 {
		t.Fatalf("bedrock concrete protocols=%v want exactly 6", bedrockProtocols)
	}
	if bedrockProtocols[0] != "responses" || bedrockProtocols[1] != "responses_stream" {
		t.Fatalf("bedrock concrete protocols=%v want responses first", bedrockProtocols)
	}
	if !SupportsProviderProtocolForSpec("bedrock", "chat_completions") || !SupportsProviderProtocolForSpec("bedrock", "messages") {
		t.Fatal("bedrock should support chat_completions and messages")
	}
	if SupportsProviderProtocolForSpec("bedrock", "converse") || SupportsProviderProtocolForSpec("bedrock", "invoke_model") {
		t.Fatal("bedrock must not advertise native runtime protocol names")
	}
	if got, ok := ResolveConcreteProtocolForAutoAtBoundary("bedrock"); !ok || got != "responses" {
		t.Fatalf("bedrock auto protocol=%q ok=%v want responses", got, ok)
	}

	azureModes := AllowedAuthModesForSpec("azure")
	if len(azureModes) != 3 {
		t.Fatalf("azure allowed auth modes=%v want exactly 3", azureModes)
	}
	if azureModes[0].Mode != AuthModeEnv || azureModes[1].Mode != AuthModeFile {
		t.Fatalf("azure allowed auth modes=%v want env/file/keychain prefix", azureModes)
	}
	if azureModes[2].Mode != AuthModeKeychain {
		t.Fatalf("azure allowed auth modes=%v want keychain third", azureModes)
	}
	if got, ok := ResolveConcreteProtocolForAutoAtBoundary("azure"); ok || got != "" {
		t.Fatalf("azure auto protocol=%q ok=%v want unresolved", got, ok)
	}
}

func TestCatalog_ProviderSetupKeywordsAreSearchOnly(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"ollama":            "model, protocol",
		"openai":            "credential, model, protocol",
		"chatgpt":           "sign in, model, protocol",
		"anthropic":         "credential, model, protocol",
		"openrouter":        "credential, model, protocol",
		"bedrock":           "region, Bedrock API key, AWS credentials, AWS profile, AWS env, AWS_BEARER_TOKEN_BEDROCK, access keys, model, protocol",
		"azure":             "endpoint, credential, deployment, protocol",
		"openai_compatible": "backend URL, credential, credential header, model, protocol",
	}

	for spec, want := range cases {
		if got := ProviderSetupKeywordSummaryForSpec(spec); got != want {
			t.Fatalf("provider setup keyword summary for %q = %q, want %q", spec, got, want)
		}
	}
}

func TestCatalog_EndpointSpecForProvider(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		kind       ProviderEndpointKind
		label      string
		defaultURL string
	}{
		"ollama":            {kind: EndpointDefaultHTTPBaseURL, label: "base URL", defaultURL: "http://127.0.0.1:11434/v1"},
		"openai":            {kind: EndpointDefaultHTTPBaseURL, label: "base URL", defaultURL: "https://api.openai.com/v1"},
		"chatgpt":           {kind: EndpointDefaultHTTPBaseURL, label: "base URL", defaultURL: "https://api.openai.com/v1"},
		"anthropic":         {kind: EndpointDefaultHTTPBaseURL, label: "base URL", defaultURL: "https://api.anthropic.com/v1"},
		"openrouter":        {kind: EndpointDefaultHTTPBaseURL, label: "base URL", defaultURL: "https://openrouter.ai/api/v1"},
		"bedrock":           {kind: EndpointRequiredHTTPBaseURL, label: "region"},
		"azure":             {kind: EndpointAzureResourceLocator, label: "project"},
		"openai_compatible": {kind: EndpointRequiredHTTPBaseURL, label: "backend URL"},
	}
	for spec, want := range cases {
		got, ok := EndpointSpecForProvider(spec)
		if !ok {
			t.Fatalf("endpoint spec for %q missing", spec)
		}
		if got.Kind != want.kind {
			t.Fatalf("endpoint kind for %q = %q, want %q", spec, got.Kind, want.kind)
		}
		if got.Label != want.label {
			t.Fatalf("endpoint label for %q = %q, want %q", spec, got.Label, want.label)
		}
		if got.DefaultURL != want.defaultURL {
			t.Fatalf("endpoint default URL for %q = %q, want %q", spec, got.DefaultURL, want.defaultURL)
		}
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

func TestCatalog_OllamaProviderProtocols_ExcludeResponses(t *testing.T) {
	t.Parallel()

	protocols := SupportedProviderProtocolsForSpec("ollama")
	if len(protocols) == 0 {
		t.Fatal("ollama protocols must not be empty")
	}
	for _, unsupported := range []string{"responses", "responses_stream"} {
		if SupportsProviderProtocolForSpec("ollama", unsupported) {
			t.Fatalf("ollama must not declare %q protocol", unsupported)
		}
	}
	for _, supported := range []string{"chat_completions", "chat_completions_stream"} {
		if !SupportsProviderProtocolForSpec("ollama", supported) {
			t.Fatalf("ollama must declare %q protocol; protocols=%v", supported, protocols)
		}
	}
	if got, ok := ResolveConcreteProtocolForAutoAtBoundary("ollama"); !ok || got != "chat_completions" {
		t.Fatalf("ollama default protocol = %q (ok=%v), want chat_completions", got, ok)
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
