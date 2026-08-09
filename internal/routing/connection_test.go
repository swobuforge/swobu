package routing

import (
	"strings"
	"testing"
)

func TestCredentialLocatorsRequirePayload(t *testing.T) {
	constructors := map[string]func(string) error{
		"openai":        func(raw string) error { _, err := NewAPIKeyConnection(ProviderOpenAI, raw); return err },
		"anthropic":     func(raw string) error { _, err := NewAPIKeyConnection(ProviderAnthropic, raw); return err },
		"openrouter":    func(raw string) error { _, err := NewAPIKeyConnection(ProviderOpenRouter, raw); return err },
		"chatgpt":       func(raw string) error { _, err := NewAPIKeyConnection(ProviderChatGPT, raw); return err },
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
	for _, raw := range []string{"env:BAD NAME", "file:relative.txt", "secret:../escape", "secretfile:chatgpt//default"} {
		if _, err := NewAPIKeyConnection(ProviderOpenAI, raw); err == nil {
			t.Errorf("NewAPIKeyConnection(%q) unexpectedly succeeded", raw)
		}
	}
	for _, raw := range []string{"env:OPENAI_API_KEY", "file:/tmp/token", "file:~/.config/swobu/token", "secret:openai/default", "secretfile:chatgpt/plus/session_1"} {
		if _, err := NewAPIKeyConnection(ProviderOpenAI, raw); err != nil {
			t.Errorf("NewAPIKeyConnection(%q): %v", raw, err)
		}
	}
}

func TestZAIAccessIsClosedAndRequired(t *testing.T) {
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
		access := test.access
		parsed, err := ParseZAIAccess(string(access))
		if err != nil {
			t.Fatalf("ParseZAIAccess(%q): %v", access, err)
		}
		connection, err := NewZAIConnection(parsed, "env:ZAI_API_KEY")
		if err != nil {
			t.Fatalf("NewZAIConnection(%q): %v", access, err)
		}
		if connection.Access() != access || access.Label() == "" || connection.BaseURL() != test.baseURL {
			t.Fatalf("connection access projection = %#v, access = %q, want base URL %q", connection, access, test.baseURL)
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
	connection, err := NewZAIConnection(ZAIAccess(" coding_plan "), "env:ZAI_API_KEY")
	if err != nil {
		t.Fatalf("whitespace access: %v", err)
	}
	if connection.Access() != ZAIAccessCodingPlan || connection.BaseURL() != "https://api.z.ai/api/coding/paas/v4" {
		t.Fatalf("whitespace access was not normalized: %#v", connection)
	}
}

func TestBedrockConnectionCarriesAuthoredEndpoint(t *testing.T) {
	region, err := ParseBedrockRegion("us-east-1")
	if err != nil {
		t.Fatalf("ParseBedrockRegion: %v", err)
	}

	// An explicit endpoint round-trips verbatim; the routing package neither
	// normalizes nor validates it as a Mantle host (that belongs to the profile
	// layer).
	for _, tc := range []struct {
		name     string
		endpoint string
	}{
		{"openai namespace", "https://bedrock-mantle.us-east-1.api.aws/openai/v1"},
		{"bare v1", "https://bedrock-mantle.us-east-1.api.aws/v1"},
		{"anthropic namespace", "https://bedrock-mantle.us-east-1.api.aws/anthropic/v1"},
		{"custom host", "https://my-proxy.internal/openai/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connection, err := NewBedrockConnection(region, tc.endpoint, "env:AWS_BEARER_TOKEN_BEDROCK")
			if err != nil {
				t.Fatalf("NewBedrockConnection(%q): %v", tc.endpoint, err)
			}
			if got := connection.Endpoint(); got != strings.TrimSpace(tc.endpoint) {
				t.Fatalf("Endpoint() = %q, want %q", got, tc.endpoint)
			}
			if connection.Region() != region {
				t.Fatalf("Region() = %#v, want %#v", connection.Region(), region)
			}
		})
	}

	// An endpoint is trimmed; whitespace is not a meaningful endpoint value.
	padded, err := NewBedrockConnection(region, "  https://bedrock-mantle.us-east-1.api.aws/openai/v1  ", "")
	if err != nil {
		t.Fatalf("padded endpoint: %v", err)
	}
	if padded.Endpoint() != "https://bedrock-mantle.us-east-1.api.aws/openai/v1" {
		t.Fatalf("endpoint was not trimmed: %q", padded.Endpoint())
	}

	// Region is still required and first-class; it is the SigV4 signing source.
	if _, err := NewBedrockConnection(BedrockRegion{}, "https://bedrock-mantle.us-east-1.api.aws/v1", ""); err == nil {
		t.Fatalf("NewBedrockConnection accepted an empty region")
	}
}

func TestBedrockConnectionRequiresAuthoredEndpoint(t *testing.T) {
	t.Parallel()

	region, err := ParseBedrockRegion("us-east-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewBedrockConnection(region, "", ""); err == nil {
		t.Fatal("NewBedrockConnection accepted an empty endpoint")
	}
}

func TestBedrockConnectionEqualityDistinguishesEndpoints(t *testing.T) {
	region, _ := ParseBedrockRegion("us-east-1")
	openai, _ := NewBedrockConnection(region, "https://bedrock-mantle.us-east-1.api.aws/openai/v1", "env:AWS_BEARER_TOKEN_BEDROCK")
	v1, _ := NewBedrockConnection(region, "https://bedrock-mantle.us-east-1.api.aws/v1", "env:AWS_BEARER_TOKEN_BEDROCK")

	cases := []struct {
		name string
		a, b Connection
		want bool
	}{
		{"same endpoint equal", openai, openai, true},
		{"openai vs v1 differ", openai, v1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := connectionsEqual(tc.a, tc.b); got != tc.want {
				t.Fatalf("connectionsEqual = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBedrockCredentialChangePreservesEndpoint(t *testing.T) {
	region, _ := ParseBedrockRegion("us-east-1")
	endpoint := "https://bedrock-mantle.us-east-1.api.aws/openai/v1"
	connection, err := NewBedrockConnection(region, endpoint, "env:AWS_BEARER_TOKEN_BEDROCK")
	if err != nil {
		t.Fatalf("NewBedrockConnection: %v", err)
	}
	updated, err := setConnectionCredential(connection, "env:AWS_BEARER_TOKEN_ROTATED")
	if err != nil {
		t.Fatalf("setConnectionCredential: %v", err)
	}
	bedrock, ok := updated.(BedrockConnection)
	if !ok {
		t.Fatalf("updated connection is %T, want BedrockConnection", updated)
	}
	if bedrock.Endpoint() != endpoint {
		t.Fatalf("credential change dropped endpoint: got %q, want %q", bedrock.Endpoint(), endpoint)
	}
	if bedrock.Region() != region {
		t.Fatalf("credential change dropped region: got %#v, want %#v", bedrock.Region(), region)
	}
}
