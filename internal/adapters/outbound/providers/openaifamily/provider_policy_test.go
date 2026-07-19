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

func TestProviderConstructors_ExposeExplicitProviderModules(t *testing.T) {
	if got := NewOpenAIPolicy().ProviderID(); got != profile.ProviderSpecOpenAI {
		t.Fatalf("openai policy provider=%s", got)
	}
	if got := NewOllamaPolicy().ProviderID(); got != profile.ProviderSpecOllama {
		t.Fatalf("ollama policy provider=%s", got)
	}
	if got := NewCustomPolicy().ProviderID(); got != profile.ProviderSpecCustom {
		t.Fatalf("custom policy provider=%s", got)
	}
	if got := NewOpenRouterPolicy().ProviderID(); got != profile.ProviderSpecOpenRouter {
		t.Fatalf("openrouter policy provider=%s", got)
	}
	if got := NewOpenAIPolicy().AuthStrategy().Style; got != AuthStyleBearer {
		t.Fatalf("openai auth style=%s", got)
	}
	if got := NewOllamaPolicy().AuthStrategy().Style; got != AuthStyleNone {
		t.Fatalf("ollama auth style=%s", got)
	}
	if got := NewCustomPolicy().AuthStrategy().Header; got != AuthHeaderAuthorization {
		t.Fatalf("custom endpoint auth header=%s", got)
	}
}

func TestProviderRoutePolicy_DecodeBuffered_UsesMandatoryProfileContract(t *testing.T) {
	raw := []byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	for _, profile := range []ProviderRoutePolicy{
		NewOpenAIPolicy(),
		NewOllamaPolicy(),
		NewCustomPolicy(),
		NewOpenRouterPolicy(),
	} {
		respResult, err := chatcompletions.ProviderDocumentDecoder{}.DecodeProviderDocument(context.Background(), carrier.Document{Family: protocolkind.ChatCompletions, Media: "application/json", Header: http.Header{}, Raw: raw}, "test_profile_decode")
		if err != nil {
			t.Fatalf("provider=%s decode: %v", profile.ProviderID(), err)
		}
		closed, err := canonical.ReadClosedEnvelope(context.Background(), respResult.Stream, canonical.EnvResponse)
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
