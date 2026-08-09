package profile

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// TestResolveBedrockEndpoint is the construction-boundary acceptance table for
// slices 070 (centralized endpoint resolution). Every row is a required
// regression from the review inventory: T-02 (one operation), T-03 (proxy
// prefix preserved in requests), T-04 (Anthropic SDK base), plus required input
// and cross-family contradiction cases.
func TestResolveBedrockEndpoint(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		endpoint string
		region   string
		kind     protocolkind.ProtocolKind
		wantBase string
		wantReq  string
		complete bool
		wantErr  bool
	}{
		{"empty endpoint is rejected", "", "us-east-1", protocolkind.Responses,
			"", "", false, true},

		// T-02: a full request URL dispatches with exactly one operation.
		{"openai base responses", "https://bedrock-mantle.us-east-1.api.aws/openai/v1", "us-east-1", protocolkind.Responses,
			"https://bedrock-mantle.us-east-1.api.aws/openai/v1",
			"https://bedrock-mantle.us-east-1.api.aws/openai/v1/responses",
			false, false},
		{"pasted full responses URL strips one operation", "https://bedrock-mantle.us-east-1.api.aws/openai/v1/responses", "us-east-1", protocolkind.Responses,
			"https://bedrock-mantle.us-east-1.api.aws/openai/v1",
			"https://bedrock-mantle.us-east-1.api.aws/openai/v1/responses",
			true, false},
		{"unresolved protocol strips full responses URL without inferring protocol", "https://proxy.example/openai/v1/responses", "us-east-1", "",
			"https://proxy.example/openai/v1",
			"",
			true, false},
		{"unresolved protocol strips full messages URL without inferring protocol", "https://proxy.example/anthropic/v1/messages", "us-east-1", "",
			"https://proxy.example/anthropic/v1",
			"",
			true, false},

		// T-03: a reverse-proxy prefix is preserved in the request.
		{"proxy prefix preserved in request", "https://proxy.example/bedrock/openai/v1", "us-east-1", protocolkind.Responses,
			"https://proxy.example/bedrock/openai/v1",
			"https://proxy.example/bedrock/openai/v1/responses",
			false, false},
		{"proxy prefix preserved with pasted operation", "https://proxy.example/bedrock/openai/v1/responses", "us-east-1", protocolkind.Responses,
			"https://proxy.example/bedrock/openai/v1",
			"https://proxy.example/bedrock/openai/v1/responses",
			true, false},

		// T-04: an Anthropic SDK base (/anthropic) promotes to /anthropic/v1.
		{"anthropic sdk base promotes to api base", "https://bedrock-mantle.us-east-1.api.aws/anthropic", "us-east-1", protocolkind.Messages,
			"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1",
			"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1/messages",
			false, false},

		{"anthropic api base messages", "https://bedrock-mantle.us-east-1.api.aws/anthropic/v1", "us-east-1", protocolkind.Messages,
			"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1",
			"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1/messages",
			false, false},

		// Contradiction: a recognized cross-family namespace vs protocol.
		{"anthropic base contradicts responses", "https://bedrock-mantle.us-east-1.api.aws/anthropic/v1", "us-east-1", protocolkind.Responses,
			"", "", false, true},
		{"openai base contradicts messages", "https://bedrock-mantle.us-east-1.api.aws/openai/v1", "us-east-1", protocolkind.Messages,
			"", "", false, true},
		{"open root contradicts messages", "https://bedrock-mantle.us-east-1.api.aws/v1", "us-east-1", protocolkind.Messages,
			"", "", false, true},
		{"proxy open root contradicts messages", "https://proxy.example/v1", "us-east-1", protocolkind.Messages,
			"", "", false, true},
		{"different protocol operation is rejected", "https://gateway.example/custom/responses", "us-east-1", protocolkind.Messages,
			"", "", false, true},
		{"model catalog URL is rejected", "https://gateway.example/custom/models", "us-east-1", protocolkind.Responses,
			"", "", false, true},
		{"canonical host region mismatch is rejected", "https://bedrock-mantle.us-east-1.api.aws/openai/v1", "eu-west-2", protocolkind.Responses,
			"", "", false, true},

		// An unrecognized path still resolves without inventing a namespace.
		{"unrecognized path resolves", "https://gateway.example/custom/v2", "us-east-1", protocolkind.Responses,
			"https://gateway.example/custom/v2",
			"https://gateway.example/custom/v2/responses",
			false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveBedrockEndpoint(tc.endpoint, tc.region, tc.kind)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ResolveBedrockEndpoint(%q, %q, %v) = %+v, want error", tc.endpoint, tc.region, tc.kind, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveBedrockEndpoint(%q, %q, %v) unexpected error = %v", tc.endpoint, tc.region, tc.kind, err)
			}
			if got.BaseURL != tc.wantBase {
				t.Errorf("BaseURL = %q, want %q", got.BaseURL, tc.wantBase)
			}
			if got.RequestURL != tc.wantReq {
				t.Errorf("RequestURL = %q, want %q", got.RequestURL, tc.wantReq)
			}
			if got.InputWasComplete != tc.complete {
				t.Errorf("InputWasComplete = %v, want %v", got.InputWasComplete, tc.complete)
			}
		})
	}
}

func TestBedrockCatalogURL(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		region string
		want   string
	}{
		{"canonical region", "us-east-1", "https://bedrock-mantle.us-east-1.api.aws/v1/models"},
		{"normalizes case and whitespace", " EU-WEST-2 ", "https://bedrock-mantle.eu-west-2.api.aws/v1/models"},
		{"empty region", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := BedrockCatalogURL(tc.region); got != tc.want {
				t.Fatalf("BedrockCatalogURL(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}
