package responses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeRequestAcceptsCodexWebSearchDeclaration(t *testing.T) {
	raw := []byte(`{"model":"default","input":"Hello World","client_metadata":{},"prompt_cache_key":"cache","include":["web_search_call.action.sources"],"tools":[{"type":"web_search","external_web_access":true,"filters":{"allowed_domains":["Example.COM"],"blocked_domains":["blocked.example"]},"user_location":{"type":"approximate","country":"GB"},"search_context_size":"medium"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	request := decoded.Request.Request
	if len(request.Tools()) != 1 || request.Tools()[0].Kind() != canonical.ToolKindWebSearch {
		t.Fatalf("tools = %#v", request.Tools())
	}
	wantSubjects := map[compat.Subject]bool{
		"wire:/include/0":                       true,
		"wire:/tools/0/external_web_access":     true,
		"wire:/tools/0/filters/allowed_domains": true,
		"wire:/tools/0/filters/blocked_domains": true,
		"wire:/tools/0/user_location":           true,
		"wire:/tools/0/search_context_size":     true,
	}
	if len(decoded.Decisions) != len(wantSubjects) {
		t.Fatalf("decisions = %#v", decoded.Decisions)
	}
	for _, decision := range decoded.Decisions {
		if decision.Feature != compat.RequestTools || decision.Outcome != compat.Drop || !wantSubjects[decision.Subject] {
			t.Fatalf("decision = %#v", decision)
		}
	}
}

func TestDecodeRequestResumesIDLessCodexWebSearchHistory(t *testing.T) {
	raw := []byte(`{
		"model":"default",
		"tools":[{"type":"web_search"}],
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"find the deadline"}]},
			{"type":"web_search_call","status":"completed","action":{"type":"search","query":"site:openai.com Build Week submission deadline","queries":["site:openai.com Build Week submission deadline"]}},
			{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"July 21"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"verify it"}]}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Request.RebasedRequest == nil {
		t.Fatal("expected completed Codex history to rebase")
	}
	items := decoded.Request.RebasedRequest.Request.Items()
	if len(items) != 1 || items[0].Kind() != canonical.ItemKindMessage {
		t.Fatalf("rebased items = %#v, want current user message only", items)
	}
}

func TestDecodeRequestRejectsIDLessCodexWebSearchHistoryWithInvalidAction(t *testing.T) {
	raw := []byte(`{
		"model":"default",
		"tools":[{"type":"web_search"}],
		"input":[
			{"type":"web_search_call","status":"completed","action":{"type":"search","url":"https://example.com/not-a-search-field"}},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]
	}`)
	_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err == nil || !strings.Contains(err.Error(), "responses request web-search history is invalid") {
		t.Fatalf("error = %v, want invalid web-search history", err)
	}
}

func TestDecodeRequestUsesActionlessCodexMarkerOnlyForHistoryPartition(t *testing.T) {
	raw := []byte(`{
		"model":"default",
		"tools":[{"type":"web_search"}],
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"find it"}]},
			{"type":"web_search_call","status":"completed"},
			{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"found"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"verify it"}]}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Request.RebasedRequest == nil {
		t.Fatal("expected actionless completed Codex history to rebase")
	}
	items := decoded.Request.RebasedRequest.Request.Items()
	message, ok := items[0].Message()
	if len(items) != 1 || !ok || message.Role() != canonical.MessageRoleUser {
		t.Fatalf("rebased canonical input = %#v, want current user item", items)
	}
}

func TestDecodeStreamingMessageUsesTerminalCitationAnnotations(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"delta\":\"£source\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"£source\",\"annotations\":[{\"type\":\"url_citation\",\"url\":\"https://example.com/x\",\"start_index\":0,\"end_index\":0}]}]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\"}}\n\n"
	reader := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	message, _ := response.Items()[0].Message()
	citations := message.Content()[0].Citations()
	if len(citations) != 1 {
		t.Fatalf("citations=%d want=1", len(citations))
	}
}

func TestDecodeStreamingCompletedWebSearchLifecyclePassesCanonicalValidation(t *testing.T) {
	declaration := canonical.NewWebSearchDeclaration()
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Tools: canonical.Specify(set)})
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"in_progress\",\"summary\":[]}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"completed\",\"summary\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"id\":\"ws_1\",\"type\":\"web_search_call\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"id\":\"ws_1\",\"type\":\"web_search_call\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"queries\":[\"deadline\"],\"sources\":[{\"type\":\"url\",\"url\":\"https://example.com/rules\",\"title\":\"Rules\"}]}}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":2,\"content_index\":0,\"delta\":\"Deadline\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":2,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"Deadline\",\"annotations\":[{\"type\":\"url_citation\",\"url\":\"https://example.com/rules\",\"title\":\"Rules\",\"start_index\":0,\"end_index\":7}]}]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"
	decoded := decodeResponseStream(request, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_web_search", nil)
	bound := canonical.NewBoundResponseIdentityStream(decoded, canonical.ResponseBinding{SwobuID: "resp_test"})
	validated := canonical.NewValidatedResponseStream(bound)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), validated, canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items()) != 3 {
		t.Fatalf("items = %d, want call, result, and message", len(response.Items()))
	}
}

func TestEncodeRequestLowersStableWebSearchTool(t *testing.T) {
	declaration := canonical.NewWebSearchDeclaration()
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Tools: canonical.Specify(set)})
	doc, err := EncodeCarrierWithDecisions(EncodeInput{Request: request}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(doc.RawBytes(), &body); err != nil {
		t.Fatal(err)
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	if tool["type"] != "web_search" || len(tool) != 1 {
		t.Fatalf("tool = %#v", tool)
	}
}

func TestDecodeBufferedWebSearchLifecycleAndUnicodeCitation(t *testing.T) {
	declaration := canonical.NewWebSearchDeclaration()
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Tools: canonical.Specify(set)})
	raw := []byte(`{"id":"resp_provider","model":"model","status":"completed","output":[{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","queries":["one","one"],"sources":[{"type":"url","url":"https://example.com/a","title":"A"}]}},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"£source","annotations":[{"type":"url_citation","url":"https://example.com/a","title":"A","start_index":0,"end_index":0}]}]}]}`)
	stream, err := decodeResponseBuffered(context.Background(), request, raw, "exchange", nil)
	if err != nil {
		t.Fatal(err)
	}
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_test", TargetID: "target", TargetVersion: 1})
	envelope, err := canonical.ReadClosedEnvelope(context.Background(), bound, canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := envelope.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 3 {
		t.Fatalf("items = %d", len(items))
	}
	call, _ := items[0].ToolCall()
	searchCall, _ := call.Input().WebSearch()
	if len(searchCall.Queries) != 2 || searchCall.Queries[0] != searchCall.Queries[1] {
		t.Fatalf("queries = %#v", searchCall.Queries)
	}
	result, _ := items[1].ToolResult()
	searchResult, ok := result.WebSearch()
	if !ok || len(searchResult.Sources()) != 1 {
		t.Fatalf("result = %#v", result)
	}
	message, _ := items[2].Message()
	citations := message.Content()[0].Citations()
	start, _ := citations[0].Start.Get()
	end, _ := citations[0].End.Get()
	if start != 0 || end != 2 {
		t.Fatalf("UTF-8 citation bytes = [%d,%d)", start, end)
	}
}

func TestDecodeCompletedWebSearchWithUndisclosedSourcesPairsEmptyResult(t *testing.T) {
	items, err := decodeResponsesWebSearchLifecycle("ws_undisclosed", json.RawMessage(`{"type":"search","queries":["deadline"],"sources":null}`), responsesWebSearchSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("lifecycle items = %d, want completed call and result", len(items))
	}
	wantCallID, _ := canonical.NewToolCallID("ws_undisclosed")
	call, ok := items[0].ToolCall()
	if !ok || call.CallID() != wantCallID {
		t.Fatalf("call = %#v", items[0])
	}
	result, ok := items[1].ToolResult()
	if !ok || result.CallID() != call.CallID() {
		t.Fatalf("result = %#v", items[1])
	}
	search, ok := result.WebSearch()
	if !ok || len(search.Sources()) != 0 {
		t.Fatalf("search result = %#v", search)
	}
}

func TestDecodeUnresolvedWebSearchWithUndisclosedSourcesRemainsCallOnly(t *testing.T) {
	items, err := decodeResponsesWebSearchLifecycle("ws_unresolved", json.RawMessage(`{"type":"search","queries":["deadline"],"sources":null}`), responsesWebSearchPending)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind() != canonical.ItemKindToolCall {
		t.Fatalf("unresolved lifecycle = %#v", items)
	}
}

func TestResponsesRequestWebSearchLifecycleRoundTripsThroughCanonical(t *testing.T) {
	tests := []struct {
		name        string
		wire        string
		wantItems   int
		wantStatus  string
		wantSources int
	}{
		{
			name:        "completed with sources",
			wire:        `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","queries":["deadline"],"sources":[{"type":"url","url":"https://example.test/rules","title":"Rules"}]}}`,
			wantItems:   2,
			wantStatus:  "completed",
			wantSources: 1,
		},
		{
			name:        "unresolved with undisclosed sources",
			wire:        `{"type":"web_search_call","id":"ws_2","status":"in_progress","action":{"type":"search","queries":["deadline"],"sources":null}}`,
			wantItems:   1,
			wantStatus:  "in_progress",
			wantSources: -1,
		},
		{
			name:        "searching alias collapses to unresolved",
			wire:        `{"type":"web_search_call","id":"ws_searching","status":"searching","action":{"type":"search","queries":["deadline"],"sources":null}}`,
			wantItems:   1,
			wantStatus:  "in_progress",
			wantSources: -1,
		},
		{
			name:        "completed with undisclosed sources",
			wire:        `{"type":"web_search_call","id":"ws_3","status":"completed","action":{"type":"search","queries":["deadline"],"sources":null}}`,
			wantItems:   2,
			wantStatus:  "completed",
			wantSources: 0,
		},
		{
			name:        "idless completed Codex replay",
			wire:        `{"type":"web_search_call","status":"completed","action":{"type":"search","queries":["deadline"],"sources":[]}}`,
			wantItems:   2,
			wantStatus:  "completed",
			wantSources: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte(`{"model":"m","input":[` + test.wire + `]}`)
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
				carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}),
			)
			if err != nil {
				t.Fatal(err)
			}
			items := decoded.Request.Request.Items()
			if len(items) != test.wantItems {
				t.Fatalf("canonical items = %d, want %d", len(items), test.wantItems)
			}
			call, ok := items[0].ToolCall()
			if !ok || call.Tool().Kind() != canonical.ToolKindWebSearch {
				t.Fatalf("canonical call = %#v", items[0])
			}
			if test.wantItems == 2 {
				result, ok := items[1].ToolResult()
				if !ok || result.CallID() != call.CallID() {
					t.Fatalf("canonical result = %#v", items[1])
				}
			}

			document, err := EncodeCarrierWithDecisions(
				EncodeInput{Request: decoded.Request.Request},
				delivery.BufferedDelivery(), nil, "", EncodeOptions{},
			)
			if err != nil {
				t.Fatal(err)
			}
			var payload struct {
				Input []json.RawMessage `json:"input"`
			}
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Input) != 1 {
				t.Fatalf("wire input = %s", document.RawBytes())
			}
			var item responsesWireOutputItemDTO
			if err := json.Unmarshal(payload.Input[0], &item); err != nil {
				t.Fatal(err)
			}
			if item.Type != "web_search_call" || item.ID != call.CallID().String() || item.Status != test.wantStatus {
				t.Fatalf("folded item = %#v", item)
			}
			var action responsesWebSearchActionDTO
			if err := json.Unmarshal(item.Action, &action); err != nil {
				t.Fatal(err)
			}
			if test.wantSources < 0 {
				if len(action.Sources) != 0 {
					t.Fatalf("unresolved action disclosed sources: %s", item.Action)
				}
				return
			}
			var sources []responsesWebSearchSourceDTO
			if err := json.Unmarshal(action.Sources, &sources); err != nil {
				t.Fatalf("completed action sources = %s: %v", action.Sources, err)
			}
			if len(sources) != test.wantSources {
				t.Fatalf("sources = %#v", sources)
			}
		})
	}
}

func TestResponsesRequestWebSearchFoldSupportsNonAdjacentResult(t *testing.T) {
	lifecycle, err := decodeResponsesWebSearchLifecycle(
		"ws_non_adjacent",
		json.RawMessage(`{"type":"search","queries":["deadline"],"sources":[{"type":"url","url":"https://example.test/rules"}]}`),
		responsesWebSearchSucceeded,
	)
	if err != nil {
		t.Fatal(err)
	}
	message, _ := canonical.NewMessageItem(
		canonical.MessageRoleAssistant,
		[]canonical.MessagePart{canonical.NewTextMessagePart("working")},
	)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{lifecycle[0], message, lifecycle[1]},
	})
	document, err := EncodeCarrierWithDecisions(
		EncodeInput{Request: request}, delivery.BufferedDelivery(), nil, "", EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Input) != 2 ||
		!strings.Contains(string(payload.Input[0]), `"type":"web_search_call"`) ||
		!strings.Contains(string(payload.Input[0]), `"status":"completed"`) ||
		!strings.Contains(string(payload.Input[1]), `"type":"message"`) {
		t.Fatalf("non-adjacent fold = %s", document.RawBytes())
	}
}

func TestResponsesFailedWebSearchHistoryIsRejectedRatherThanMadePending(t *testing.T) {
	requestRaw := []byte(`{"model":"m","input":[{"type":"web_search_call","id":"ws_failed","status":"failed","action":{"type":"search","queries":["deadline"]}}]}`)
	_, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, requestRaw, carrier.Meta{}),
	)
	var clientError canonical.Error
	if !errors.As(err, &clientError) || clientError.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("failed client history error = %#v, want %s", err, canonical.ErrorCodeBadRequest)
	}

	providerRaw := []byte(`{"id":"resp_1","model":"m","status":"completed","output":[{"type":"web_search_call","id":"ws_failed","status":"failed","action":{"type":"search","queries":["deadline"]}}]}`)
	_, err = decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, providerRaw, "ex", nil)
	var providerError canonical.Error
	if !errors.As(err, &providerError) || providerError.Code != canonical.ErrorCodeNotImplemented {
		t.Fatalf("failed provider history error = %#v, want %s", err, canonical.ErrorCodeNotImplemented)
	}
}
