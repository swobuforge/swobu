package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestResponsesLowersLateDirectiveRoleWithoutReorderingHistory(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "Hello World"),
			canonicaltest.Message(t, canonical.MessageRoleSystem, "late Claude directive"),
		},
	})

	document, changes := encodeResponsesRequestWithChanges(t, NewRuntime(nil, nil).BackendResolver, profile.ProviderSpecOllama, request)
	var payload struct {
		Input []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(document, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Input) != 2 || payload.Input[0].Role != "user" || payload.Input[1].Role != "user" {
		t.Fatalf("Ollama input roles = %#v, want user then lowered user", payload.Input)
	}
	if fmt.Sprint(payload.Input[0].Content) != "Hello World" || fmt.Sprint(payload.Input[1].Content) != "late Claude directive" {
		t.Fatalf("Ollama input content or order changed: %#v", payload.Input)
	}
	want := compat.NewApproximation(canonical.RequestItemsMessageRole, canonical.RequestItemOccurrence(1))
	if len(changes) != 1 || changes[0] != want {
		t.Fatalf("Ollama changes = %#v, want %#v", changes, []compat.Change{want})
	}
	for _, field := range []string{"thread_tail_role=system", "encoded_tail_role=user"} {
		if !strings.Contains(logs.String(), field) {
			t.Fatalf("Ollama request-shape logs missing %q after role lowering: %s", field, logs.String())
		}
	}

	standardDocument := encodeResponsesRequest(t, openaifamily.NewRuntime(nil, nil, openaifamily.StandardBearerPolicy(profile.ProviderSpecOpenAI)).BackendResolver, profile.ProviderSpecOpenAI, request)
	var standardPayload struct {
		Input []struct {
			Role string `json:"role"`
		} `json:"input"`
	}
	if err := json.Unmarshal(standardDocument, &standardPayload); err != nil {
		t.Fatal(err)
	}
	if len(standardPayload.Input) != 2 || standardPayload.Input[1].Role != "system" {
		t.Fatalf("standard Responses input changed: %s", standardDocument)
	}
}

func TestResponsesResumeLowersHistoricalCustomThroughPortableFunction(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "shell")
	declaration := canonicaltest.MustCustomTool(key, "Run shell text", canonicaltest.MustToolFormat(`{"type":"text"}`))
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewTextToolInput("echo exact"))
	callValue, _ := call.ToolCall()
	result, err := canonical.NewToolResultItem(callValue.CallID(), []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, declaration),
			call,
			result,
			canonicaltest.Message(t, canonical.MessageRoleUser, "continue"),
		},
	})

	wire := string(encodeResponsesRequest(t, NewRuntime(nil, nil).BackendResolver, profile.ProviderSpecOllama, request))
	for _, forbidden := range []string{`"type":"custom"`, `"type":"custom_tool_call"`, `"type":"custom_tool_call_output"`} {
		if strings.Contains(wire, forbidden) {
			t.Fatalf("Ollama resume leaked native Responses Custom syntax %s: %s", forbidden, wire)
		}
	}
	for _, want := range []string{`"type":"function"`, `"type":"function_call"`, `"type":"function_call_output"`, `"arguments":"{\"input\":\"echo exact\"}"`} {
		if !strings.Contains(wire, want) {
			t.Fatalf("Ollama resume = %s, want portable Custom projection %s", wire, want)
		}
	}
}

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
	document, _ := encodeResponsesRequestWithChanges(t, resolver, providerID, request)
	return document
}

func encodeResponsesRequestWithChanges(t *testing.T, resolver provider.BackendResolver, providerID profile.ProviderID, request canonical.CanonicalRequest) ([]byte, []compat.Change) {
	t.Helper()
	target := provider.NewTargetSnapshot("target", string(providerID), "http://127.0.0.1:11434/v1", "", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.Model = request.Model()
	backend, err := resolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, changes, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery(), ToolNames: names})
	if err != nil {
		t.Fatal(err)
	}
	return document.RawBytes(), changes
}
