package exchange

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestProviderTargetFromCustomConnectionPreservesProviderIdentity(t *testing.T) {
	provider, _ := routing.ParseProvider("custom", func(candidate string) bool { return candidate == "custom" })
	connection, err := routing.NewCustomConnection(provider, "https://example.test/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{"responses", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			target, err := ProviderTargetFromConnection("custom-a", connection, protocol)
			if err != nil {
				t.Fatal(err)
			}
			if target.ProviderSpec != "custom" {
				t.Fatalf("provider spec = %q, want custom", target.ProviderSpec)
			}
		})
	}
}

func TestProviderTargetFromLMStudioConnectionUsesProfileDefaultsAndOptionalCredential(t *testing.T) {
	provider, _ := routing.ParseProvider("lmstudio", func(candidate string) bool { return candidate == "lmstudio" })
	connection, err := routing.NewStandardConnection(provider, "", "env:LM_API_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	target, err := ProviderTargetFromConnection("lm-studio-a", connection, "responses")
	if err != nil {
		t.Fatal(err)
	}
	if target.ProviderSpec != "lmstudio" || target.BaseURL != "http://127.0.0.1:1234/v1" || target.CredentialRef != "env:LM_API_TOKEN" {
		t.Fatalf("LM Studio target = %#v", target)
	}
}

func TestProviderTargetFromTogetherConnectionUsesFixedChatEndpoint(t *testing.T) {
	provider, _ := routing.ParseProvider("together", func(candidate string) bool { return candidate == "together" })
	connection, err := routing.NewStandardConnection(provider, "", "env:TOGETHER_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	target, err := ProviderTargetFromConnection("together-a", connection, "chat_completions_stream")
	if err != nil {
		t.Fatal(err)
	}
	if target.ProviderSpec != "together" || target.BaseURL != "https://api.together.ai/v1" || target.CredentialRef != "env:TOGETHER_API_KEY" {
		t.Fatalf("Together AI target = %#v", target)
	}
}

func TestProviderTargetFromFriendliConnectionPreservesExactEndpointFacts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		baseURL    string
		credential string
		protocol   string
		wantURL    string
	}{
		{name: "serverless default", credential: "env:FRIENDLI_TOKEN", protocol: "chat_completions_stream", wantURL: "https://api.friendli.ai/serverless/v1"},
		{name: "dedicated", baseURL: "https://api.friendli.ai/dedicated/v1", credential: "env:FRIENDLI_TOKEN", protocol: "responses_stream", wantURL: "https://api.friendli.ai/dedicated/v1"},
		{name: "container without auth", baseURL: "http://127.0.0.1:8000/v1", protocol: "messages_stream", wantURL: "http://127.0.0.1:8000/v1"},
		{name: "arbitrary gateway", baseURL: "https://friendli-gateway.example/v1", protocol: "chat_completions_stream", wantURL: "https://friendli-gateway.example/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, _ := routing.ParseProvider("friendli", func(candidate string) bool { return candidate == "friendli" })
			connection, err := routing.NewStandardConnection(provider, tc.baseURL, tc.credential)
			if err != nil {
				t.Fatal(err)
			}
			target, err := ProviderTargetFromConnection("friendli", connection, tc.protocol)
			if err != nil {
				t.Fatal(err)
			}
			if target.ProviderSpec != "friendli" || target.BaseURL != tc.wantURL || target.CredentialRef != tc.credential || target.ProviderProtocol != tc.protocol {
				t.Fatalf("Friendli target = %#v", target)
			}
		})
	}
}

func TestProviderTargetFromNebiusConnectionPreservesPublicAndDedicatedFacts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		baseURL    string
		model      string
		protocol   string
		wantURL    string
		credential string
	}{
		{name: "public default", model: "meta-llama/Llama-3.3-70B-Instruct", protocol: "responses_stream", wantURL: "https://api.tokenfactory.nebius.com/v1", credential: "env:NEBIUS_API_KEY"},
		{name: "dedicated routing key", baseURL: "https://api.tokenfactory.us-central1.nebius.com/v1", model: "dedicated-routing-key", protocol: "chat_completions_stream", wantURL: "https://api.tokenfactory.us-central1.nebius.com/v1", credential: "env:NEBIUS_API_KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, _ := routing.ParseProvider("nebius", func(candidate string) bool { return candidate == "nebius" })
			connection, err := routing.NewStandardConnection(provider, tc.baseURL, tc.credential)
			if err != nil {
				t.Fatal(err)
			}
			target, err := ProviderTargetFromConnection("nebius", connection, tc.protocol)
			if err != nil {
				t.Fatal(err)
			}
			if target.ProviderSpec != "nebius" || target.BaseURL != tc.wantURL || target.CredentialRef != tc.credential || target.ProviderProtocol != tc.protocol {
				t.Fatalf("Nebius target = %#v", target)
			}
		})
	}
}

func TestProviderTargetProjectionCarriesRoutingIdentityAndVersion(t *testing.T) {
	target := requestpathTarget(t, "custom-a")
	snapshot, err := toProviderTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TargetID != target.ID().String() || snapshot.TargetVersion != uint64(target.Version()) {
		t.Fatalf("target snapshot = %#v", snapshot)
	}
}

// Bedrock target construction is the first protocol-aware validity boundary.
// It normalizes explicit endpoints, derives protocol-specific defaults, and
// rejects canonical host/signing-region contradictions before a snapshot exists.
func TestProviderTargetFromBedrockConnectionCarriesEndpointAndSigningRegion(t *testing.T) {
	region, err := routing.ParseBedrockRegion("eu-west-2")
	if err != nil {
		t.Fatal(err)
	}
	responsesStream := "responses_stream"

	t.Run("authored endpoint is the execution base URL", func(t *testing.T) {
		const endpoint = "https://bedrock-mantle.eu-west-2.api.aws/openai/v1"
		provider, _ := routing.ParseProvider("bedrock", func(candidate string) bool { return candidate == "bedrock" })
		connection, err := routing.NewBedrockConnection(provider, region, endpoint, "")
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := ProviderTargetFromConnection("grok-a", connection, responsesStream)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.BaseURL != endpoint {
			t.Fatalf("base URL = %q, want authored endpoint %q", snapshot.BaseURL, endpoint)
		}
		if snapshot.BedrockRegion() != region.String() {
			t.Fatalf("BedrockRegion = %q, want %q (signing source)", snapshot.BedrockRegion(), region.String())
		}
	})

	t.Run("full request URL normalizes before snapshot construction", func(t *testing.T) {
		provider, _ := routing.ParseProvider("bedrock", func(candidate string) bool { return candidate == "bedrock" })
		connection, err := routing.NewBedrockConnection(provider, region, "https://bedrock-mantle.eu-west-2.api.aws/openai/v1/responses", "")
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := ProviderTargetFromConnection("grok-normalized", connection, responsesStream)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.BaseURL != "https://bedrock-mantle.eu-west-2.api.aws/openai/v1" {
			t.Fatalf("normalized base URL = %q", snapshot.BaseURL)
		}
	})

	t.Run("canonical host region mismatch is rejected before snapshot", func(t *testing.T) {
		provider, _ := routing.ParseProvider("bedrock", func(candidate string) bool { return candidate == "bedrock" })
		connection, err := routing.NewBedrockConnection(provider, region, "https://bedrock-mantle.us-east-1.api.aws/openai/v1", "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = ProviderTargetFromConnection("grok-mismatch", connection, responsesStream)
		if err == nil || !strings.Contains(err.Error(), "does not match signing region") {
			t.Fatalf("mismatch error = %v", err)
		}
	})
}
