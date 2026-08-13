package routing

import (
	"strings"
	"testing"
)

func supportedProvider(raw string) Provider {
	provider, err := ParseProvider(raw, func(candidate string) bool { return candidate != "" })
	if err != nil {
		panic(err)
	}
	return provider
}

func TestCredentialLocatorsRequirePayload(t *testing.T) {
	constructors := map[string]func(string) error{
		"standard": func(raw string) error {
			_, err := NewStandardConnection(supportedProvider("openai"), "", raw)
			return err
		},
		"custom header": func(raw string) error { _, err := NewCustomHeaderAuth("Authorization", raw); return err },
	}
	for name, construct := range constructors {
		for _, raw := range []string{"env:", "file:", "secret:", "secretfile:"} {
			if err := construct(raw); err == nil {
				t.Errorf("%s accepted empty locator %q", name, raw)
			}
		}
	}
}

func TestCredentialLocatorsMatchResolverSyntax(t *testing.T) {
	provider := supportedProvider("openai")
	for _, raw := range []string{"env:BAD NAME", "file:relative.txt", "secret:../escape", "secretfile:chatgpt//default"} {
		if _, err := NewStandardConnection(provider, "", raw); err == nil {
			t.Errorf("NewStandardConnection(%q) unexpectedly succeeded", raw)
		}
	}
	for _, raw := range []string{"env:OPENAI_API_KEY", "file:/tmp/token", "file:~/.config/swobu/token", "secret:openai/default", "secretfile:chatgpt/plus/session_1"} {
		if _, err := NewStandardConnection(provider, "", raw); err != nil {
			t.Errorf("NewStandardConnection(%q): %v", raw, err)
		}
	}
}

func TestStandardConnectionPreservesProviderAndDurableFacts(t *testing.T) {
	t.Parallel()

	providers := []string{"lmstudio", "vllm", "nebius", "gmi", "gemini", "scaleway", "sambanova", "stepfun", "groq", "fireworks", "ollama", "azure"}
	connections := make([]StandardConnection, 0, len(providers))
	for _, raw := range providers {
		connection, err := NewStandardConnection(supportedProvider(raw), "https://models.example/v1", "env:PROVIDER_TOKEN")
		if err != nil {
			t.Fatalf("NewStandardConnection(%q): %v", raw, err)
		}
		if connection.Provider() != Provider(raw) {
			t.Fatalf("provider = %q, want %q", connection.Provider(), raw)
		}
		connections = append(connections, connection)
	}
	for i := 1; i < len(connections); i++ {
		if connectionsEqual(connections[i-1], connections[i]) {
			t.Fatalf("distinct provider identities compared equal: %q and %q", connections[i-1].Provider(), connections[i].Provider())
		}
	}
}

func TestParseProviderUsesConstructionSupport(t *testing.T) {
	if _, err := ParseProvider("futurecloud", func(string) bool { return false }); err == nil {
		t.Fatal("unsupported provider was accepted")
	}
	provider, err := ParseProvider(" futurecloud ", func(raw string) bool { return raw == "futurecloud" })
	if err != nil || provider != Provider("futurecloud") {
		t.Fatalf("ParseProvider = %q, %v", provider, err)
	}
}

// TestFinalizeTargetAcceptsFutureStandardProviderWithoutProviderArm is the
// N+1 falsification: once the construction edge admits a provider as Standard,
// routing needs no identifier constant, connection variant, or finalizer arm.
func TestFinalizeTargetAcceptsFutureStandardProviderWithoutProviderArm(t *testing.T) {
	target, err := FinalizeTarget(TargetDraft{
		ID:       "futurecloud-default",
		Model:    "future-model",
		Protocol: "chat_completions",
		Connection: ConnectionDraft{
			Provider: "futurecloud",
			Standard: &StandardConnectionDraft{
				Locator:    "https://api.futurecloud.example/v1",
				Credential: "env:FUTURECLOUD_API_KEY",
			},
		},
	}, TargetConstructionFacts{
		ProviderSupported: func(raw string) bool { return raw == "futurecloud" },
		ConnectionShape: func(provider Provider) (ConnectionShape, bool) {
			return ConnectionShapeStandard, provider == "futurecloud"
		},
		ValidateStandardConnection: func(_ Provider, draft StandardConnectionDraft) (StandardConnectionDraft, error) {
			return draft, nil
		},
		ProtocolSupported: func(provider Provider, protocol string) bool {
			return provider == "futurecloud" && protocol == "chat_completions"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider() != Provider("futurecloud") {
		t.Fatalf("provider = %q, want futurecloud", target.Provider())
	}
	connection, ok := target.Connection().(StandardConnection)
	if !ok {
		t.Fatalf("connection = %T, want StandardConnection", target.Connection())
	}
	locator, ok := connection.Locator()
	if !ok {
		t.Fatal("futurecloud standard connection omitted its authored locator")
	}
	if got := locator.String(); got != "https://api.futurecloud.example/v1" {
		t.Fatalf("locator = %q", got)
	}
	if got := connection.Credential().String(); got != "env:FUTURECLOUD_API_KEY" {
		t.Fatalf("credential = %q", got)
	}
}

func TestFinalizeConnectionRejectsStandardProviderWithoutConstructionValidator(t *testing.T) {
	_, err := FinalizeConnection(ConnectionDraft{
		Provider: "futurecloud",
		Standard: &StandardConnectionDraft{
			Locator:    "https://api.futurecloud.example/v1",
			Credential: "env:FUTURECLOUD_API_KEY",
		},
	}, TargetConstructionFacts{
		ProviderSupported: func(raw string) bool { return raw == "futurecloud" },
		ConnectionShape: func(Provider) (ConnectionShape, bool) {
			return ConnectionShapeStandard, true
		},
	})
	if err == nil {
		t.Fatal("Standard provider finalized without construction validation")
	}
	if !strings.Contains(err.Error(), "connection.provider") || !strings.Contains(err.Error(), "provider connection validation is unavailable") {
		t.Fatalf("missing construction validator error = %v", err)
	}
}

func TestFutureStandardProviderUsesConstructionValidatorWithoutRoutingMembership(t *testing.T) {
	validate := func(_ Provider, draft StandardConnectionDraft) (StandardConnectionDraft, error) {
		if draft.Credential == "" {
			return StandardConnectionDraft{}, pathError("connection.futurecloud.credential", "is required")
		}
		if draft.Locator != "" {
			return StandardConnectionDraft{}, pathError("connection.futurecloud.base_url", "is not authorable")
		}
		return draft, nil
	}
	facts := TargetConstructionFacts{
		ProviderSupported:          func(raw string) bool { return raw == "futurecloud" },
		ConnectionShape:            func(Provider) (ConnectionShape, bool) { return ConnectionShapeStandard, true },
		ValidateStandardConnection: validate,
		ProtocolSupported:          func(Provider, string) bool { return true },
	}
	for name, draft := range map[string]ConnectionDraft{
		"missing required credential": {Provider: "futurecloud", Standard: &StandardConnectionDraft{}},
		"forbidden fixed base URL":    {Provider: "futurecloud", Standard: &StandardConnectionDraft{Locator: "https://override.example/v1", Credential: "env:FUTURECLOUD_API_KEY"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := FinalizeConnection(draft, facts); err == nil {
				t.Fatal("futurecloud invalid Standard draft was accepted")
			}
		})
	}
	if _, err := FinalizeConnection(ConnectionDraft{Provider: "futurecloud", Standard: &StandardConnectionDraft{Credential: "env:FUTURECLOUD_API_KEY"}}, facts); err != nil {
		t.Fatalf("futurecloud valid Standard draft: %v", err)
	}
}

func TestConnectionValidationErrorsStaySemantic(t *testing.T) {
	_, err := FinalizeConnection(ConnectionDraft{
		Provider: "futurecloud",
		ZAI:      &ZAIConnectionDraft{Access: "coding_plan", Credential: "env:FUTURECLOUD_API_KEY"},
	}, TargetConstructionFacts{
		ProviderSupported: func(raw string) bool { return raw == "futurecloud" },
		ConnectionShape:   func(Provider) (ConnectionShape, bool) { return ConnectionShapeStandard, true },
	})
	if err == nil {
		t.Fatal("mismatched provider connection was accepted")
	}
	if strings.Contains(strings.ToLower(err.Error()), "connection shape") {
		t.Fatalf("connection validation leaked implementation term: %v", err)
	}
	if !strings.Contains(err.Error(), "provider connection details are required") {
		t.Fatalf("connection validation error = %v", err)
	}
}

func TestZAIAccessIsClosedAndRequired(t *testing.T) {
	provider := supportedProvider("zai")
	accesses := []struct {
		access  ZAIAccess
		baseURL string
	}{
		{ZAIAccessGeneralAPI, "https://api.z.ai/api/paas/v4"},
		{ZAIAccessCodingPlan, "https://api.z.ai/api/coding/paas/v4"},
	}
	if got := ZAIAccesses(); len(got) != len(accesses) {
		t.Fatalf("Z.AI accesses = %#v", got)
	}
	for _, test := range accesses {
		parsed, err := ParseZAIAccess(string(test.access))
		if err != nil {
			t.Fatalf("ParseZAIAccess(%q): %v", test.access, err)
		}
		connection, err := NewZAIConnection(provider, parsed, "env:ZAI_API_KEY")
		if err != nil {
			t.Fatalf("NewZAIConnection(%q): %v", test.access, err)
		}
		if connection.Access() != test.access || test.access.Label() == "" || connection.BaseURL() != test.baseURL {
			t.Fatalf("connection access projection = %#v, access = %q, want base URL %q", connection, test.access, test.baseURL)
		}
	}
	for _, raw := range []string{"", "default", "coding"} {
		if _, err := ParseZAIAccess(raw); err == nil {
			t.Errorf("ParseZAIAccess(%q) unexpectedly succeeded", raw)
		}
	}
	if got := ZAIAccess("future").Label(); got != "" {
		t.Fatalf("unknown access label = %q", got)
	}
	if got := (ZAIConnection{}).BaseURL(); got != "" {
		t.Fatalf("zero connection base URL = %q", got)
	}
	connection, err := NewZAIConnection(provider, ZAIAccess(" coding_plan "), "env:ZAI_API_KEY")
	if err != nil {
		t.Fatalf("whitespace access: %v", err)
	}
	if connection.Access() != ZAIAccessCodingPlan || connection.BaseURL() != "https://api.z.ai/api/coding/paas/v4" {
		t.Fatalf("whitespace access was not normalized: %#v", connection)
	}
}

func TestBedrockConnectionCarriesAuthoredEndpoint(t *testing.T) {
	provider := supportedProvider("bedrock")
	region, err := ParseBedrockRegion("us-east-1")
	if err != nil {
		t.Fatalf("ParseBedrockRegion: %v", err)
	}
	for _, endpoint := range []string{
		"https://bedrock-mantle.us-east-1.api.aws/openai/v1",
		"https://bedrock-mantle.us-east-1.api.aws/v1",
		"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1",
		"https://my-proxy.internal/openai/v1",
	} {
		connection, err := NewBedrockConnection(provider, region, endpoint, "env:AWS_BEARER_TOKEN_BEDROCK")
		if err != nil {
			t.Fatalf("NewBedrockConnection(%q): %v", endpoint, err)
		}
		if got := connection.Endpoint(); got != endpoint {
			t.Fatalf("Endpoint() = %q, want %q", got, endpoint)
		}
	}
	padded, err := NewBedrockConnection(provider, region, "  https://bedrock-mantle.us-east-1.api.aws/openai/v1  ", "")
	if err != nil || padded.Endpoint() != "https://bedrock-mantle.us-east-1.api.aws/openai/v1" {
		t.Fatalf("padded endpoint = %#v, %v", padded, err)
	}
	if _, err := NewBedrockConnection(provider, BedrockRegion{}, "https://bedrock-mantle.us-east-1.api.aws/v1", ""); err == nil {
		t.Fatal("NewBedrockConnection accepted an empty region")
	}
}

func TestBedrockConnectionEqualityIncludesCredential(t *testing.T) {
	provider := supportedProvider("bedrock")
	region, _ := ParseBedrockRegion("us-east-1")
	endpoint := "https://bedrock-mantle.us-east-1.api.aws/openai/v1"
	connection, _ := NewBedrockConnection(provider, region, endpoint, "env:AWS_BEARER_TOKEN_BEDROCK")
	other, _ := NewBedrockConnection(provider, region, "https://bedrock-mantle.us-east-1.api.aws/v1", "env:AWS_BEARER_TOKEN_BEDROCK")
	if !connectionsEqual(connection, connection) || connectionsEqual(connection, other) {
		t.Fatal("Bedrock durable equality does not distinguish endpoints")
	}
	rotated, err := NewBedrockConnection(provider, region, endpoint, "env:AWS_BEARER_TOKEN_ROTATED")
	if err != nil {
		t.Fatal(err)
	}
	if connectionsEqual(connection, rotated) {
		t.Fatal("Bedrock durable equality ignored credential rotation")
	}
}

func TestStandardConnectionKeepsOptionalLocatorAndCredential(t *testing.T) {
	connection, err := NewStandardConnection(supportedProvider("ollama"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := connection.Locator(); ok || connection.Credential().String() != "" {
		t.Fatalf("empty standard connection = %#v", connection)
	}
	if _, err := NewStandardConnection(supportedProvider("openai"), "not a URL", ""); err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("invalid locator error = %v", err)
	}
}
