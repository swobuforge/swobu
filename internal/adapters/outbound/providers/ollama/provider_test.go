package ollama

import (
	"encoding/json"
	"testing"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestHistoricalSearchDoesNotRequireCurrentSearchAndInstructionsStayFirst(t *testing.T) {
	request := historicalSearchRequest(t)
	ollamaDocument := encodeResponsesRequest(t, NewRuntime(nil, nil).BackendResolver, profile.ProviderSpecOllama, request)
	standardDocument := encodeResponsesRequest(t, openaifamily.NewRuntime(nil, nil, openaifamily.StandardBearerPolicy(profile.ProviderSpecOpenAI)).BackendResolver, profile.ProviderSpecOpenAI, request)

	var ollamaPayload struct {
		Instructions any              `json:"instructions"`
		Tools        any              `json:"tools"`
		Input        []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(ollamaDocument, &ollamaPayload); err != nil {
		t.Fatal(err)
	}
	if ollamaPayload.Instructions != nil || ollamaPayload.Tools != nil {
		t.Fatalf("Ollama payload has current instructions/tools fields: %s", ollamaDocument)
	}
	if len(ollamaPayload.Input) < 2 || ollamaPayload.Input[0]["role"] != "system" {
		t.Fatalf("Ollama input does not start with system instructions: %#v", ollamaPayload.Input)
	}
	searchSeen := false
	for _, item := range ollamaPayload.Input {
		if item["type"] == "web_search_call" {
			searchSeen = item["status"] == "completed"
		}
	}
	if !searchSeen {
		t.Fatalf("completed historical search was not retained: %s", ollamaDocument)
	}

	var standardPayload map[string]any
	if err := json.Unmarshal(standardDocument, &standardPayload); err != nil {
		t.Fatal(err)
	}
	if standardPayload["instructions"] != "current instructions" {
		t.Fatalf("standard Responses instruction placement changed: %s", standardDocument)
	}
}

func historicalSearchRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	callID, _ := canonical.NewToolCallID("search_1")
	input, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"deadline"}})
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	result, _ := canonical.NewWebSearchResult(nil)
	resultItem, err := canonical.NewWebSearchResultItem(callID, result)
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.MustInstruction(canonical.MessageRoleSystem, "current instructions"),
			canonicaltest.Message(t, canonical.MessageRoleUser, "find the deadline"),
			call,
			resultItem,
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "Hosted search answer"),
			canonicaltest.Message(t, canonical.MessageRoleUser, "explain that answer"),
		},
	})
}

func encodeResponsesRequest(t *testing.T, resolver provider.BackendResolver, providerID profile.ProviderID, request canonical.CanonicalRequest) []byte {
	t.Helper()
	target := provider.NewTargetSnapshot("target", string(providerID), "http://127.0.0.1:11434/v1", "", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.Model = request.Model()
	backend, err := resolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	return document.RawBytes()
}
