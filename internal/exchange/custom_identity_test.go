package exchange

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestProviderTargetFromCustomConnectionPreservesProviderIdentity(t *testing.T) {
	connection, err := routing.NewCustomConnection("https://example.test/v1", nil)
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
	connection, err := routing.NewEndpointCredentialConnection(routing.ProviderLMStudio, "", "env:LM_API_TOKEN")
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
		connection, err := routing.NewBedrockConnection(region, endpoint, "")
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
		connection, err := routing.NewBedrockConnection(region, "https://bedrock-mantle.eu-west-2.api.aws/openai/v1/responses", "")
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
		connection, err := routing.NewBedrockConnection(region, "https://bedrock-mantle.us-east-1.api.aws/openai/v1", "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = ProviderTargetFromConnection("grok-mismatch", connection, responsesStream)
		if err == nil || !strings.Contains(err.Error(), "does not match signing region") {
			t.Fatalf("mismatch error = %v", err)
		}
	})
}
