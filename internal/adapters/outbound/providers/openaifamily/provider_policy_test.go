package openaifamily

import (
	"context"
	"net/http"
	"testing"

	chatcompletions "github.com/swobuforge/swobu/internal/adapters/wire/families/chatcompletions"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestProviderConstructors_ExposeExplicitProviderModules(t *testing.T) {
	if got := NewOpenAIPolicy().ProviderID(); got != profile.ProviderSpecOpenAI {
		t.Fatalf("openai policy provider=%s", got)
	}
	if got := NewOllamaPolicy().ProviderID(); got != profile.ProviderSpecOllama {
		t.Fatalf("ollama policy provider=%s", got)
	}
	if got := NewOpenAICompatiblePolicy().ProviderID(); got != profile.ProviderSpecOpenAICompatible {
		t.Fatalf("openaicompat policy provider=%s", got)
	}
	if got := NewAzurePolicy().ProviderID(); got != profile.ProviderSpecAzure {
		t.Fatalf("azure policy provider=%s", got)
	}
	if got := NewOpenRouterPolicy().ProviderID(); got != profile.ProviderSpecOpenRouter {
		t.Fatalf("openrouter policy provider=%s", got)
	}
	if got := NewOpenAIPolicy().AuthStrategy().Style; got != authStyleBearer {
		t.Fatalf("openai auth style=%s", got)
	}
	if got := NewOllamaPolicy().AuthStrategy().Style; got != authStyleNone {
		t.Fatalf("ollama auth style=%s", got)
	}
	if got := NewOpenAICompatiblePolicy().AuthStrategy().Header; got != authHeaderAuthorization {
		t.Fatalf("openai-compatible auth header=%s", got)
	}
	if got := NewAzurePolicy().AuthStrategy().Header; got != authHeaderAPIKey {
		t.Fatalf("azure auth header=%s", got)
	}
}

func TestProviderRoutePolicy_DecodeBuffered_UsesMandatoryProfileContract(t *testing.T) {
	raw := []byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	for _, profile := range []ProviderRoutePolicy{
		NewOpenAIPolicy(),
		NewOllamaPolicy(),
		NewOpenAICompatiblePolicy(),
		NewOpenRouterPolicy(),
	} {
		respResult, err := chatcompletions.ProviderDocumentDecoder{}.DecodeProviderDocument(context.Background(), carrier.WireDocument{Stage: carrier.StageProviderIngressIn, Family: protocolkind.ChatCompletions, Media: "application/json", Header: http.Header{}, Raw: raw}, "test_profile_decode")
		if err != nil {
			t.Fatalf("provider=%s decode: %v", profile.ProviderID(), err)
		}
		closed, err := canonical.ReadClosedEnvelope(context.Background(), respResult.Value, canonical.EnvResponse)
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
