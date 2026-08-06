package profile

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// TestResolveBedrockEndpoint is the construction-boundary acceptance table for
// slices 070 (centralized endpoint resolution). Every row is a required
// regression from the review inventory: T-02 (one operation), T-03 (proxy
// prefix preserved in request and catalog), T-04 (Anthropic SDK base), plus the
// regional default and cross-family contradiction cases.
func TestResolveBedrockEndpoint(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		endpoint string
		region   string
		kind     protocolkind.ProtocolKind
		wantBase string
		wantReq  string
		wantCat  string
		complete bool
		wantErr  bool
	}{
		// Regional default: empty explicit endpoint derives the bare open root.
		{"empty endpoint derives regional default", "", "us-east-1", protocolkind.Responses,
			"https://bedrock-mantle.us-east-1.api.aws/v1",
			"https://bedrock-mantle.us-east-1.api.aws/v1/responses",
			"https://bedrock-mantle.us-east-1.api.aws/v1/models", false, false},
		{"empty endpoint derives messages namespace", "", "us-east-1", protocolkind.Messages,
			"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1",
			"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1/messages",
			"https://bedrock-mantle.us-east-1.api.aws/v1/models", false, false},

		// T-02: a full request URL dispatches with exactly one operation.
		{"openai base responses", "https://bedrock-mantle.us-east-1.api.aws/openai/v1", "us-east-1", protocolkind.Responses,
			"https://bedrock-mantle.us-east-1.api.aws/openai/v1",
			"https://bedrock-mantle.us-east-1.api.aws/openai/v1/responses",
			"https://bedrock-mantle.us-east-1.api.aws/v1/models", false, false},
		{"pasted full responses URL strips one operation", "https://bedrock-mantle.us-east-1.api.aws/openai/v1/responses", "us-east-1", protocolkind.Responses,
			"https://bedrock-mantle.us-east-1.api.aws/openai/v1",
			"https://bedrock-mantle.us-east-1.api.aws/openai/v1/responses",
			"https://bedrock-mantle.us-east-1.api.aws/v1/models", true, false},
		{"unresolved protocol strips full responses URL without inferring protocol", "https://proxy.example/openai/v1/responses", "us-east-1", "",
			"https://proxy.example/openai/v1",
			"",
			"https://proxy.example/v1/models", true, false},
		{"unresolved protocol strips full messages URL without inferring protocol", "https://proxy.example/anthropic/v1/messages", "us-east-1", "",
			"https://proxy.example/anthropic/v1",
			"",
			"https://proxy.example/v1/models", true, false},

		// T-03: a reverse-proxy prefix is preserved in request and catalog.
		{"proxy prefix preserved in request and catalog", "https://proxy.example/bedrock/openai/v1", "us-east-1", protocolkind.Responses,
			"https://proxy.example/bedrock/openai/v1",
			"https://proxy.example/bedrock/openai/v1/responses",
			"https://proxy.example/bedrock/v1/models", false, false},
		{"proxy prefix preserved with pasted operation", "https://proxy.example/bedrock/openai/v1/responses", "us-east-1", protocolkind.Responses,
			"https://proxy.example/bedrock/openai/v1",
			"https://proxy.example/bedrock/openai/v1/responses",
			"https://proxy.example/bedrock/v1/models", true, false},

		// T-04: an Anthropic SDK base (/anthropic) promotes to /anthropic/v1.
		{"anthropic sdk base promotes to api base", "https://bedrock-mantle.us-east-1.api.aws/anthropic", "us-east-1", protocolkind.Messages,
			"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1",
			"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1/messages",
			"https://bedrock-mantle.us-east-1.api.aws/v1/models", false, false},

		// Namespace collapse: catalog always reaches the bare /v1 service root.
		{"anthropic api base messages", "https://bedrock-mantle.us-east-1.api.aws/anthropic/v1", "us-east-1", protocolkind.Messages,
			"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1",
			"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1/messages",
			"https://bedrock-mantle.us-east-1.api.aws/v1/models", false, false},

		// Contradiction: a recognized cross-family namespace vs protocol.
		{"anthropic base contradicts responses", "https://bedrock-mantle.us-east-1.api.aws/anthropic/v1", "us-east-1", protocolkind.Responses,
			"", "", "", false, true},
		{"openai base contradicts messages", "https://bedrock-mantle.us-east-1.api.aws/openai/v1", "us-east-1", protocolkind.Messages,
			"", "", "", false, true},
		{"open root contradicts messages", "https://bedrock-mantle.us-east-1.api.aws/v1", "us-east-1", protocolkind.Messages,
			"", "", "", false, true},
		{"proxy open root contradicts messages", "https://proxy.example/v1", "us-east-1", protocolkind.Messages,
			"", "", "", false, true},
		{"different protocol operation is rejected", "https://gateway.example/custom/responses", "us-east-1", protocolkind.Messages,
			"", "", "", false, true},
		{"model catalog URL is rejected", "https://gateway.example/custom/models", "us-east-1", protocolkind.Responses,
			"", "", "", false, true},
		{"canonical host region mismatch is rejected", "https://bedrock-mantle.us-east-1.api.aws/openai/v1", "eu-west-2", protocolkind.Responses,
			"", "", "", false, true},

		// An unrecognized path (custom gateway, no known namespace) still resolves
		// a base and request URL, but yields no catalog (fall back to typed model).
		{"unrecognized path has no catalog", "https://gateway.example/custom/v2", "us-east-1", protocolkind.Responses,
			"https://gateway.example/custom/v2",
			"https://gateway.example/custom/v2/responses",
			"", false, false},
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
			if got.CatalogURL != tc.wantCat {
				t.Errorf("CatalogURL = %q, want %q", got.CatalogURL, tc.wantCat)
			}
			if got.InputWasComplete != tc.complete {
				t.Errorf("InputWasComplete = %v, want %v", got.InputWasComplete, tc.complete)
			}
		})
	}
}

// TestEffectiveBedrockAPIURL covers the presentation projection that the Cockpit
// row displays. It must never blank a malformed explicit value (rejection is the
// validator's job) and an empty endpoint must follow the region.
func TestEffectiveBedrockAPIURL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		region   string
		endpoint string
		kind     protocolkind.ProtocolKind
		want     string
	}{
		{"empty endpoint derives responses default", "us-east-1", "", protocolkind.Responses, "https://bedrock-mantle.us-east-1.api.aws/v1"},
		{"empty endpoint derives messages default", "us-east-1", "", protocolkind.Messages, "https://bedrock-mantle.us-east-1.api.aws/anthropic/v1"},
		{"whitespace-only endpoint derives regional default", "us-east-1", "   ", protocolkind.Responses, "https://bedrock-mantle.us-east-1.api.aws/v1"},
		{"derived endpoint follows a different region", "eu-west-2", "", protocolkind.Responses, "https://bedrock-mantle.eu-west-2.api.aws/v1"},
		{"explicit openai base is normalized", "us-east-1", "https://bedrock-mantle.us-east-1.api.aws/openai/v1", protocolkind.Responses, "https://bedrock-mantle.us-east-1.api.aws/openai/v1"},
		{"pasted full request URL strips one operation", "us-east-1", "https://bedrock-mantle.us-east-1.api.aws/openai/v1/responses", protocolkind.Responses, "https://bedrock-mantle.us-east-1.api.aws/openai/v1"},
		{"proxy prefix is preserved", "us-east-1", "https://proxy.example/bedrock/openai/v1", protocolkind.Responses, "https://proxy.example/bedrock/openai/v1"},
		{"region mismatch remains visible for validation", "eu-west-2", "https://bedrock-mantle.us-east-1.api.aws/openai/v1", protocolkind.Responses, "https://bedrock-mantle.us-east-1.api.aws/openai/v1"},
		{"malformed explicit endpoint is preserved verbatim, not blanked", "us-east-1", "not-a-url", protocolkind.Responses, "not-a-url"},
		{"empty region with empty endpoint yields empty effective", "", "", protocolkind.Responses, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := EffectiveBedrockAPIURL(tc.region, tc.endpoint, tc.kind); got != tc.want {
				t.Fatalf("EffectiveBedrockAPIURL(%q, %q, %q) = %q, want %q", tc.region, tc.endpoint, tc.kind, got, tc.want)
			}
		})
	}
}

// TestCanonicalBedrockEndpointIntent covers endpoint submission. Pasting the
// regional default must collapse to empty so it cannot create a sticky override.
func TestCanonicalBedrockEndpointIntent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		region   string
		endpoint string
		kind     protocolkind.ProtocolKind
		want     string
	}{
		{"empty input stays empty (derived)", "us-east-1", "", protocolkind.Responses, ""},
		{"explicit override persists normalized", "us-east-1", "https://bedrock-mantle.us-east-1.api.aws/openai/v1", protocolkind.Responses, "https://bedrock-mantle.us-east-1.api.aws/openai/v1"},
		{"pasted full URL collapses to normalized base", "us-east-1", "https://bedrock-mantle.us-east-1.api.aws/openai/v1/responses", protocolkind.Responses, "https://bedrock-mantle.us-east-1.api.aws/openai/v1"},
		{"responses default collapses to empty", "us-east-1", "https://bedrock-mantle.us-east-1.api.aws/v1", protocolkind.Responses, ""},
		{"responses request default collapses to empty", "us-east-1", "https://bedrock-mantle.us-east-1.api.aws/v1/responses", protocolkind.Responses, ""},
		{"messages default collapses to empty", "us-east-1", "https://bedrock-mantle.us-east-1.api.aws/anthropic/v1", protocolkind.Messages, ""},
		{"proxy endpoint persists with its prefix", "us-east-1", "https://proxy.example/bedrock/openai/v1", protocolkind.Responses, "https://proxy.example/bedrock/openai/v1"},
		{"malformed input is preserved verbatim for the validator", "us-east-1", "not-a-url", protocolkind.Responses, "not-a-url"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CanonicalBedrockEndpointIntent(tc.region, tc.endpoint, tc.kind); got != tc.want {
				t.Fatalf("CanonicalBedrockEndpointIntent(%q, %q, %q) = %q, want %q", tc.region, tc.endpoint, tc.kind, got, tc.want)
			}
		})
	}
}
