package openaifamily

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
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

func TestWorkersAIPolicyAppliesDefaultGatewayHeaderOnlyToItsGenerationRoute(t *testing.T) {
	workers := WorkersAIPolicy()
	if workers.ProviderID() != profile.ProviderSpecWorkersAI || workers.AuthStrategy() != BearerAuthStrategy() {
		t.Fatalf("Workers AI policy = %#v", workers)
	}
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses} {
		headers := http.Header{}
		workers.ApplyProtocolHeaders(kind, "token", headers)
		if headers.Get("cf-aig-gateway-id") != "default" {
			t.Fatalf("%s Workers AI headers = %#v", kind, headers)
		}
	}
	standardHeaders := http.Header{}
	StandardBearerPolicy(profile.ProviderSpecOpenAI).ApplyProtocolHeaders(protocolkind.ChatCompletions, "token", standardHeaders)
	if !reflect.DeepEqual(standardHeaders, http.Header{}) {
		t.Fatalf("Workers AI header leaked to standard provider: %#v", standardHeaders)
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
		doc := carrier.NewDocument(protocolkind.ChatCompletions, "application/json", http.Header{}, raw, carrier.Meta{})
		codec := protocolcodec.Codec{Protocol: protocolkind.ChatCompletions}
		respResult, err := codec.Decode(context.Background(), provider.Request{Attempt: provider.AttemptContext{ExchangeID: "test_profile_decode"}}, provider.DocumentIngress{Document: doc})
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
