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
	if !SupportsSpec("custom") {
		t.Fatal("custom provider spec should be supported")
	}
	obsoleteIdentity := "openai_" + "compatible"
	if SupportsSpec(obsoleteIdentity) {
		t.Fatal("obsolete custom-endpoint provider identity must fail closed")
	}
}

func TestCustomEndpointLoopbackCredentialPolicyParsesHostname(t *testing.T) {
	spec := string(ProviderSpecCustom)
	for _, raw := range []string{"http://localhost:8080/v1", "http://127.0.0.1:8080/v1", "http://[::1]:8080/v1"} {
		if RequiresCredential(spec, raw) {
			t.Errorf("RequiresCredential(%q) = true, want false", raw)
		}
	}
	for _, raw := range []string{"http://localhost.evil.example/v1", "http://127.0.0.1.evil.example/v1", "https://localhost/v1", "not a url"} {
		if !RequiresCredential(spec, raw) {
			t.Errorf("RequiresCredential(%q) = false, want true", raw)
		}
	}
}

func TestCatalog_DefaultsAndCredentialPolicy(t *testing.T) {
	t.Parallel()

	if got := DefaultExecuteBaseURL("chatgpt"); got != "https://api.openai.com/v1" {
		t.Fatalf("chatgpt default base URL = %q", got)
	}
	if RequiresCredential("chatgpt", DefaultExecuteBaseURL("chatgpt")) {
		t.Fatal("chatgpt login is not a generic credential requirement")
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
	if RequiresCredential("custom", "http://localhost:9999/v1") {
		t.Fatal("localhost custom endpoint should not require credential")
	}
	if !RequiresCredential("custom", "https://lab.example/v1") {
		t.Fatal("remote custom endpoint should require credential")
	}
	if !RequiresLocator("azure") {
		t.Fatal("azure should require an explicit endpoint")
	}
	if !RequiresLocator("bedrock") {
		t.Fatal("bedrock should require an explicit endpoint")
	}
	if got := DefaultEnvKeyForSpec("azure"); got != "AZURE_OPENAI_API_KEY" {
		t.Fatalf("azure default env key = %q", got)
	}
	if got := DefaultEnvKeyForSpec("bedrock"); got != "AWS_BEARER_TOKEN_BEDROCK" {
		t.Fatalf("bedrock default env key = %q", got)
	}
	if got := DefaultExecuteBaseURL("zai"); got != "" {
		t.Fatalf("Z.AI must not have an access-independent endpoint, got %q", got)
	}
	if got := ConcreteProviderProtocolsForSpec("zai"); len(got) != 1 || got[0] != "chat_completions_stream" {
		t.Fatalf("Z.AI protocols = %#v, want fixed streaming Chat Completions", got)
	}
	if got := DefaultAuthHeaderForSpec("custom"); got != "Authorization" {
		t.Fatalf("custom endpoint default auth header = %q", got)
	}
	authHeaders := SupportedAuthHeadersForSpec("custom")
	if len(authHeaders) != 3 {
		t.Fatalf("custom endpoint auth headers=%v want 3 common choices", authHeaders)
	}
	if authHeaders[0] != "Authorization" || authHeaders[1] != "x-api-key" || authHeaders[2] != "api-key" {
		t.Fatalf("custom endpoint auth headers=%v want [Authorization x-api-key api-key]", authHeaders)
	}
	if got := ConcreteProviderProtocolsForSpec("custom"); len(got) != 6 {
		t.Fatalf("custom endpoint concrete protocols=%v want exactly 6", got)
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

	if got, ok := ResolveConcreteProtocolForAutoAtBoundary("azure"); ok || got != "" {
		t.Fatalf("azure auto protocol=%q ok=%v want unresolved", got, ok)
	}
}

func TestCatalog_ProviderSetupKeywordsAreSearchOnly(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"ollama":     "model, protocol",
		"openai":     "credential, model, protocol",
		"chatgpt":    "sign in, model, protocol",
		"anthropic":  "credential, model, protocol",
		"openrouter": "credential, model, protocol",
		"bedrock":    "region, Bedrock API key, AWS credentials, model, protocol",
		"azure":      "endpoint, credential, deployment, protocol",
		"custom":     "backend URL, credential, credential header, model, protocol",
	}

	for spec, want := range cases {
		if got := ProviderSetupKeywordSummaryForSpec(spec); got != want {
			t.Fatalf("provider setup keyword summary for %q = %q, want %q", spec, got, want)
		}
	}
}

func TestCatalog_ProviderAuthoringMatrix(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		locator    LocatorSpec
		credential CredentialSpec
		noun       string
	}{
		"ollama":     {LocatorSpec{Kind: LocatorBaseURL, Label: "base URL", Default: "http://127.0.0.1:11434/v1"}, CredentialSpec{Requirement: CredentialUnsupported}, "model"},
		"openai":     {LocatorSpec{Kind: LocatorFixed, Default: "https://api.openai.com/v1"}, CredentialSpec{Requirement: CredentialRequired, SuggestedEnvVar: "OPENAI_API_KEY"}, "model"},
		"chatgpt":    {LocatorSpec{Kind: LocatorFixed, Default: "https://api.openai.com/v1"}, CredentialSpec{Requirement: CredentialUnsupported}, "model"},
		"anthropic":  {LocatorSpec{Kind: LocatorFixed, Default: "https://api.anthropic.com/v1"}, CredentialSpec{Requirement: CredentialRequired, SuggestedEnvVar: "ANTHROPIC_API_KEY"}, "model"},
		"openrouter": {LocatorSpec{Kind: LocatorFixed, Default: "https://openrouter.ai/api/v1"}, CredentialSpec{Requirement: CredentialRequired, SuggestedEnvVar: "OPENROUTER_API_KEY"}, "model"},
		"bedrock":    {LocatorSpec{Kind: LocatorAWSRegion, Label: "region"}, CredentialSpec{Requirement: CredentialOptional, SuggestedEnvVar: "AWS_BEARER_TOKEN_BEDROCK"}, "model"},
		"azure":      {LocatorSpec{Kind: LocatorAzureProject, Label: "project"}, CredentialSpec{Requirement: CredentialRequired, SuggestedEnvVar: "AZURE_OPENAI_API_KEY"}, "deployment"},
		"custom":     {LocatorSpec{Kind: LocatorBaseURL, Label: "backend URL"}, CredentialSpec{Requirement: CredentialRequiredOutsideLoopback}, "model"},
	}
	for spec, want := range cases {
		got, ok := LocatorSpecForProvider(spec)
		if !ok {
			t.Fatalf("locator spec for %q missing", spec)
		}
		if got != want.locator {
			t.Fatalf("locator for %q = %+v, want %+v", spec, got, want.locator)
		}
		provider, ok := profileFor(spec)
		if !ok {
			t.Fatalf("profile for %q missing", spec)
		}
		if provider.Credential != want.credential {
			t.Fatalf("credential for %q = %+v, want %+v", spec, provider.Credential, want.credential)
		}
		if got := CatalogItemLabelForSpec(spec); got != want.noun {
			t.Fatalf("catalog noun for %q = %q, want %q", spec, got, want.noun)
		}
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
