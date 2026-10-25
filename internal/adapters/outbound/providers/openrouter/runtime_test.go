package openrouter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestNewRuntime_BindsOpenRouterProviderID(t *testing.T) {
	rt := NewRuntime(nil, nil)
	if rt.ProviderID != profile.ProviderSpecOpenRouter {
		t.Fatalf("provider id = %s, want %s", rt.ProviderID, profile.ProviderSpecOpenRouter)
	}
}

func TestResponsesHostedSearchDropsCodexAccessRestriction(t *testing.T) {
	search := canonical.NewWebSearchDeclaration()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, search),
			canonicaltest.Message(t, canonical.MessageRoleUser, "search"),
		},
	})
	target := provider.NewTargetSnapshot(
		"openrouter", string(profile.ProviderSpecOpenRouter), "https://openrouter.example/api/v1", "env:TOKEN",
		protocolkind.Responses, "responses", delivery.BufferedDelivery(),
	)
	target.Model = request.Model()
	backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Tools) != 1 || payload.Tools[0]["type"] != "openrouter:web_search" {
		t.Fatalf("OpenRouter Responses tools = %#v", payload.Tools)
	}
	if strings.Contains(string(document.RawBytes()), "external_web_access") {
		t.Fatalf("OpenRouter Responses search retained unsupported access restriction: %s", document.RawBytes())
	}
}
