package openaifamily

import (
	"context"
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	chatcompletions "github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

// TestStandardBearerPolicyAdmitsFutureProviderWithoutNewPolicyType is the
// policy-specific N+1 proof: provider identity is explicit composition data,
// not a new empty type in the shared outbound kernel.
func TestStandardBearerPolicyAdmitsFutureProviderWithoutNewPolicyType(t *testing.T) {
	policy := StandardBearerPolicy(profile.ProviderID("futurecloud"))
	if got := policy.ProviderID(); got != "futurecloud" {
		t.Fatalf("provider = %q", got)
	}
	if got := policy.AuthStrategy(); got != BearerAuthStrategy() {
		t.Fatalf("auth = %#v, want Bearer", got)
	}
	if got := policy.ModelCatalogDialect(); got != ModelCatalogOpenAI {
		t.Fatalf("catalog dialect = %d, want OpenAI", got)
	}
}

func TestPolicyConstructorsRetainOnlyRealRouteDifferences(t *testing.T) {
	if got := StandardNoAuthPolicy(profile.ProviderSpecOllama).AuthStrategy().Style; got != AuthStyleNone {
		t.Fatalf("Ollama auth style = %s", got)
	}
	if got := LMStudioPolicy().ModelCatalogDialect(); got != ModelCatalogLMStudioV1 {
		t.Fatalf("LM Studio catalog dialect = %d", got)
	}
	if got := APIKeyPolicy(profile.ProviderSpecAzure).AuthStrategy(); got != APIKeyAuthStrategy() {
		t.Fatalf("Azure auth = %#v, want api-key", got)
	}
	if got := GMIPolicy().ProviderID(); got != profile.ProviderSpecGMI {
		t.Fatalf("GMI provider = %q", got)
	}
}

func TestProviderRoutePolicy_DecodeBuffered_UsesMandatoryProfileContract(t *testing.T) {
	raw := []byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	for _, profile := range []ProviderRoutePolicy{
		StandardBearerPolicy(profile.ProviderSpecOpenAI),
		StandardNoAuthPolicy(profile.ProviderSpecOllama),
		LMStudioPolicy(),
		StandardBearerPolicy(profile.ProviderSpecVLLM),
		StandardBearerPolicy(profile.ProviderSpecCustom),
		StandardBearerPolicy(profile.ProviderSpecOpenRouter),
		StandardBearerPolicy(profile.ProviderSpecKimi),
		StandardBearerPolicy(profile.ProviderSpecNebius),
	} {
		respResult, err := chatcompletions.ProviderDocumentDecoder{}.DecodeProviderDocument(context.Background(), canonical.CanonicalRequest{}, nil, carrier.Document{Family: protocolkind.ChatCompletions, Media: "application/json", Header: http.Header{}, Raw: raw}, "test_profile_decode")
		if err != nil {
			t.Fatalf("provider=%s decode: %v", profile.ProviderID(), err)
		}
		closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(respResult.Stream, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
		if err != nil {
			t.Fatalf("provider=%s envelope read: %v", profile.ProviderID(), err)
		}
		out, err := closed.ProjectResponse()
		if err != nil {
			t.Fatalf("provider=%s envelope projection: %v", profile.ProviderID(), err)
		}
		if out.Model() != "m" {
			t.Fatalf("provider=%s model=%q", profile.ProviderID(), out.Model())
		}
	}
}
