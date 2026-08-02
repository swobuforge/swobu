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
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestDecodeRequestApproximatesWebSearchQualityHints(t *testing.T) {
	raw := []byte(`{"model":"default","input":"Hello World","client_metadata":{},"prompt_cache_key":"cache","include":["web_search_call.action.sources"],"tools":[{"type":"web_search","external_web_access":true,"user_location":{"type":"approximate","country":"GB"},"search_context_size":"medium"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	request := decoded.Request.Request
	if len(canonicaltest.Tools(request)) != 1 || canonicaltest.Tools(request)[0].Kind() != canonical.ToolKindWebSearch {
		t.Fatalf("tools = %#v", canonicaltest.Tools(request))
	}
	want := []compat.Kind{compat.Omission, compat.Approximation}
	if len(decoded.Changes) != len(want) {
		t.Fatalf("changes = %#v", decoded.Changes)
	}
	for index, decision := range decoded.Changes {
		toolIndex, occurrenceOK := decision.Occurrence.ToolIndex()
		if decision.Capability != canonical.RequestToolsKind || decision.Kind != want[index] || !occurrenceOK || toolIndex != 0 {
			t.Fatalf("decision = %#v", decision)
		}
	}
}

func TestDecodeRequestClassifiesFutureWebSearchEnumsLocally(t *testing.T) {
	tests := []struct {
		name        string
		tool        string
		wantTools   int
		wantOutcome compat.Kind
	}{
		{name: "content discriminator erases operation", tool: `{"type":"web_search","search_content_types":["video"]}`, wantOutcome: compat.Omission},
		{name: "location discriminator erases operation", tool: `{"type":"web_search","user_location":{"type":"future_location"}}`, wantOutcome: compat.Omission},
		{name: "context quality hint approximates field", tool: `{"type":"web_search","search_context_size":"ultra"}`, wantTools: 1, wantOutcome: compat.Approximation},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte(`{"model":"default","input":"search","tools":[` + test.tool + `]}`)
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(canonicaltest.Tools(decoded.Request.Request)); got != test.wantTools {
				t.Fatalf("tool count = %d, want %d", got, test.wantTools)
			}
			if len(decoded.Changes) != 1 || decoded.Changes[0].Kind != test.wantOutcome {
				t.Fatalf("changes = %#v, want one %v", decoded.Changes, test.wantOutcome)
			}
		})
	}
}

func TestDecodeRequestDropsWebSearchWithUnrepresentedConstraints(t *testing.T) {
	tests := []struct {
		name string
		tool string
	}{
		{name: "allowed domains", tool: `{"type":"web_search","filters":{"allowed_domains":["example.com"]}}`},
		{name: "blocked domains", tool: `{"type":"web_search","filters":{"blocked_domains":["blocked.example"]}}`},
		{name: "external access denied", tool: `{"type":"web_search","external_web_access":false}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte(`{"model":"default","input":"search","tools":[` + test.tool + `]}`)
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
			if err != nil {
				t.Fatal(err)
			}
			if len(canonicaltest.Tools(decoded.Request.Request)) != 0 ||
				len(decoded.Changes) != 1 || decoded.Changes[0].Kind != compat.Omission {
				t.Fatalf("projection = tools %#v changes %#v", canonicaltest.Tools(decoded.Request.Request), decoded.Changes)
			}
		})
	}
}

func TestDecodeRequestDropsUnrepresentedImageSearchOperation(t *testing.T) {
	tests := []struct {
		name string
		tool string
	}{
		{name: "image only", tool: `{"type":"web_search","search_content_types":["image"]}`},
		{name: "mixed text and image", tool: `{"type":"web_search","search_content_types":["text","image"]}`},
		{name: "image output format", tool: `{"type":"web_search","output_format":"image"}`},
		{name: "future image result format", tool: `{"type":"web_search","output_format":"future_image_result"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte(`{"model":"default","input":"search","tools":[` + test.tool + `]}`)
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
			if err != nil {
				t.Fatal(err)
			}
			if len(canonicaltest.Tools(decoded.Request.Request)) != 0 ||
				len(decoded.Changes) != 1 || decoded.Changes[0].Kind != compat.Omission {
				t.Fatalf("projection = tools %#v changes %#v", canonicaltest.Tools(decoded.Request.Request), decoded.Changes)
			}
		})
	}
}

func TestDecodeRequestPreservesSupportedToolBesideDroppedConstrainedSearch(t *testing.T) {
	raw := []byte(`{
		"model":"default",
		"input":"search",
		"tools":[
			{"type":"web_search","filters":{"allowed_domains":["example.com"]}},
			{"type":"function","name":"lookup","parameters":{"type":"object"}}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	tools := canonicaltest.Tools(decoded.Request.Request)
	if len(tools) != 1 || tools[0].Key().Name() != "lookup" {
		t.Fatalf("surviving tools = %#v, want lookup", tools)
	}
	if len(decoded.Changes) != 1 || decoded.Changes[0].Kind != compat.Omission {
		t.Fatalf("changes = %#v, want constrained-search Drop", decoded.Changes)
	}
}

func TestDecodeRequestRejectsSpecificSelectionOfDroppedSearch(t *testing.T) {
	raw := []byte(`{
		"model":"default",
		"input":"search",
		"tools":[{"type":"web_search","filters":{"allowed_domains":["example.com"]}}],
		"tool_choice":{"type":"web_search"}
	}`)
	_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	var canonicalErr canonical.Error
	if !errors.As(err, &canonicalErr) || canonicalErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error = %T %v, want residual BAD_REQUEST", err, err)
	}
}

func TestDecodeRequestIgnoresUnknownIncludeEntry(t *testing.T) {
	raw := []byte(`{"model":"default","input":"hello","include":["future.include","reasoning.encrypted_content"]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Changes) != 0 {
		t.Fatalf("wire-only include created semantic changes: %#v", decoded.Changes)
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
	if len(items) != 2 || items[0].Kind() != canonical.ItemKindToolDeclarations || items[1].Kind() != canonical.ItemKindMessage {
		t.Fatalf("rebased items = %#v, want request declarations and current user message", items)
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
	message, ok := items[1].Message()
	if len(items) != 2 || items[0].Kind() != canonical.ItemKindToolDeclarations || !ok || message.Role() != canonical.MessageRoleUser {
		t.Fatalf("rebased canonical input = %#v, want request declarations and current user item", items)
	}
}

func TestDecodeStreamingMessageUsesTerminalCitationAnnotations(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"delta\":\"£source\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"£source\",\"annotations\":[{\"type\":\"url_citation\",\"url\":\"https://example.com/x\",\"start_index\":0,\"end_index\":0}]}]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\"}}\n\n"
	reader := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
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

func TestEncodeStreamingMessagePreservesTerminalCitationAnnotations(t *testing.T) {
	webURL, err := canonical.NewWebURL("https://example.com/x")
	if err != nil {
		t.Fatal(err)
	}
	source, err := canonical.NewWebSource(webURL, canonical.Specify("Source"))
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewCitedTextMessagePart("£source", []canonical.WebCitation{{Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{part})
	if err != nil {
		t.Fatal(err)
	}
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")},
		"model", []canonical.CanonicalItem{message}, canonical.Completed("stop"), canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	events := canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"exchange", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage(),
	))
	encoded, err := (ResponseStreamEncoder{}).EncodeResponseStream(
		context.Background(), canonical.CanonicalRequest{}, events, delivery.StreamingDelivery(delivery.FramingSSE),
	)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(encoded.Stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	if !strings.Contains(wire, `"annotations":[{"type":"url_citation","url":"https://example.com/x","title":"Source"`) {
		t.Fatalf("stream terminal output lost citation annotations: %s", wire)
	}
}

func TestDecodeStreamingCompletedWebSearchLifecyclePassesCanonicalValidation(t *testing.T) {
	declaration := canonical.NewWebSearchDeclaration()
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	if err != nil {
		t.Fatal(err)
	}
	declarations, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarations}})
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"in_progress\",\"summary\":[]}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"completed\",\"summary\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"id\":\"ws_1\",\"type\":\"web_search_call\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"id\":\"ws_1\",\"type\":\"web_search_call\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"queries\":[\"deadline\"],\"sources\":[{\"type\":\"url\",\"url\":\"https://example.com/rules\",\"title\":\"Rules\"}]}}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":2,\"content_index\":0,\"delta\":\"Deadline\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":2,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"Deadline\",\"annotations\":[{\"type\":\"url_citation\",\"url\":\"https://example.com/rules\",\"title\":\"Rules\",\"start_index\":0,\"end_index\":7}]}]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"
	decoded := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_web_search", nil, true)
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
	declarations, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{declarations}})
	doc, err := EncodeCarrierWithChanges(EncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
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
	declarations, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarations}})
	raw := []byte(`{"id":"resp_provider","model":"model","status":"completed","output":[{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","queries":["one","one"],"sources":[{"type":"url","url":"https://example.com/a","title":"A"}]}},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"£source","annotations":[{"type":"url_citation","url":"https://example.com/a","title":"A","start_index":0,"end_index":0}]}]}]}`)
	stream, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), raw, "exchange", nil, true)
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

func TestDecodeCompletedResponsesClassifiesMalformedWebSearchProviderDataAsBackend(t *testing.T) {
	tests := []struct {
		name string
		item string
	}{
		{
			name: "missing call id",
			item: `{"type":"web_search_call","status":"completed","action":{"type":"search","queries":["q"]}}`,
		},
		{
			name: "malformed action",
			item: `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"future_action"}}`,
		},
		{
			name: "invalid action URL",
			item: `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"open_page","url":"not-a-url"}}`,
		},
		{
			name: "malformed sources",
			item: `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","queries":["q"],"sources":"bad"}}`,
		},
		{
			name: "invalid source URL",
			item: `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","queries":["q"],"sources":[{"type":"url","url":"not-a-url"}]}}`,
		},
		{
			name: "invalid citation URL",
			item: `{"type":"message","status":"completed","content":[{"type":"output_text","text":"answer","annotations":[{"type":"url_citation","url":"not-a-url"}]}]}`,
		},
		{
			name: "invalid citation range",
			item: `{"type":"message","status":"completed","content":[{"type":"output_text","text":"answer","annotations":[{"type":"url_citation","url":"https://example.com","start_index":4,"end_index":8}]}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeCompletedResponsesItemSet(
				context.Background(), canonical.CanonicalRequest{}, nil, []json.RawMessage{json.RawMessage(test.item)},
				"",
				"ex",
				nil,
			)
			var backendErr canonical.BackendError
			if !errors.As(err, &backendErr) {
				t.Fatalf("error = %T %v, want backend error", err, err)
			}
		})
	}
}

func TestResponsesWebSearchCallerMalformationRemainsBadRequest(t *testing.T) {
	_, err := decodeResponsesWebSearchLifecycle(
		"ws_1",
		json.RawMessage(`{"type":"open_page","url":"not-a-url"}`),
		responsesWebSearchSucceeded,
	)
	var canonicalErr canonical.Error
	if !errors.As(err, &canonicalErr) || canonicalErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error = %T %v, want BAD_REQUEST", err, err)
	}
}

func TestDecodeResponsesStreamClassifiesMalformedWebSearchSourceAsBackend(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"web_search_call\",\"id\":\"ws_1\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"queries\":[\"q\"],\"sources\":[{\"type\":\"url\",\"url\":\"not-a-url\"}]}}}\n\n"
	stream := decodeResponseStream(
		canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex",
		nil, true,
	)
	var backendErr canonical.BackendError
	if err := drainResponsesStream(stream); !errors.As(err, &backendErr) {
		t.Fatalf("error = %T %v, want backend error", err, err)
	}
}

func TestResponsesRequestWebSearchLifecycleRoundTripsThroughCanonical(t *testing.T) {
	tests := []struct {
		name        string
		wire        string
		wantItems   int
		wantStatus  string
		wantSources int
		// wantID is the exact Responses item id that must survive the round trip,
		// or "" when the provider omitted it. An omitted id must re-encode with no
		// id at all — never with the canonical correlation token minted into it.
		wantID string
	}{
		{
			name:        "completed with sources",
			wire:        `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","queries":["deadline"],"sources":[{"type":"url","url":"https://example.test/rules","title":"Rules"}]}}`,
			wantItems:   2,
			wantStatus:  "completed",
			wantSources: 1,
			wantID:      "ws_1",
		},
		{
			name:        "unresolved with undisclosed sources",
			wire:        `{"type":"web_search_call","id":"ws_2","status":"in_progress","action":{"type":"search","queries":["deadline"],"sources":null}}`,
			wantItems:   1,
			wantStatus:  "in_progress",
			wantSources: -1,
			wantID:      "ws_2",
		},
		{
			name:        "searching alias collapses to unresolved",
			wire:        `{"type":"web_search_call","id":"ws_searching","status":"searching","action":{"type":"search","queries":["deadline"],"sources":null}}`,
			wantItems:   1,
			wantStatus:  "in_progress",
			wantSources: -1,
			wantID:      "ws_searching",
		},
		{
			name:        "completed with undisclosed sources",
			wire:        `{"type":"web_search_call","id":"ws_3","status":"completed","action":{"type":"search","queries":["deadline"],"sources":null}}`,
			wantItems:   2,
			wantStatus:  "completed",
			wantSources: 0,
			wantID:      "ws_3",
		},
		{
			name:        "idless completed Codex replay",
			wire:        `{"type":"web_search_call","status":"completed","action":{"type":"search","queries":["deadline"],"sources":[]}}`,
			wantItems:   2,
			wantStatus:  "completed",
			wantSources: 0,
			wantID:      "",
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

			document, err := EncodeCarrierWithChanges(
				EncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)},
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
			if item.Type != "web_search_call" || item.Status != test.wantStatus {
				t.Fatalf("folded item = %#v", item)
			}
			// The canonical correlation token is never a Responses item id: even
			// when the call has no preserved refinement it must not appear as id.
			if item.ID != test.wantID {
				t.Fatalf("folded item id = %q, want %q (correlation %q must not leak): %s", item.ID, test.wantID, call.CallID().String(), payload.Input[0])
			}
			// Negative assertion: no synthetic canonical correlation ID may ever
			// populate a Responses item.id, regardless of the input shape.
			if strings.Contains(string(payload.Input[0]), "toolu_swobu_") {
				t.Fatalf("synthetic canonical correlation leaked into responses item: %s", payload.Input[0])
			}
			var action responsesWebSearchActionDTO
			if err := json.Unmarshal(item.Action, &action); err != nil {
				t.Fatal(err)
			}
			if len(action.Sources) != 0 {
				t.Fatalf("replayed action disclosed sources: %s", item.Action)
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
	document, err := EncodeCarrierWithChanges(
		EncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{},
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

func TestResponsesFailedWebSearchHistoryPreservesTypedFailure(t *testing.T) {
	requestRaw := []byte(`{"model":"m","input":[{"type":"web_search_call","id":"ws_failed","status":"failed","action":{"type":"search","queries":["deadline"]}}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, requestRaw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	assertResponsesFailedWebSearchItems(t, decoded.Request.Request.Items())

	providerRaw := []byte(`{"id":"resp_1","model":"m","status":"completed","output":[{"type":"web_search_call","id":"ws_failed","status":"failed","action":{"type":"search","queries":["deadline"]}}]}`)
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, providerRaw, "ex", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(
		context.Background(),
		canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}),
		canonical.EnvResponse,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	assertResponsesFailedWebSearchItems(t, response.Items())
}

func assertResponsesFailedWebSearchItems(t *testing.T, items []canonical.CanonicalItem) {
	t.Helper()
	if len(items) != 2 || items[0].Kind() != canonical.ItemKindToolCall {
		t.Fatalf("failed lifecycle items = %#v", items)
	}
	result, ok := items[1].ToolResult()
	if !ok {
		t.Fatalf("failed lifecycle result = %#v", items[1])
	}
	search, ok := result.WebSearch()
	failure, failed := search.Failure()
	if !ok || !failed || failure != "provider reported failed web search" {
		t.Fatalf("typed failure = (%q,%t,%t)", failure, failed, ok)
	}
}

func TestResponsesWebSearchFailedAndIncompleteBufferedStreamParity(t *testing.T) {
	tests := []struct {
		name       string
		buffered   string
		streamed   string
		wantItems  int
		wantFailed bool
	}{
		{
			name: "failed",
			buffered: `{"id":"resp_1","model":"m","status":"completed","output":[` +
				`{"type":"web_search_call","id":"ws_1","status":"failed","action":{"type":"search","queries":["deadline"]}}]}`,
			streamed: "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"web_search_call\",\"id\":\"ws_1\",\"status\":\"failed\",\"action\":{\"type\":\"search\",\"queries\":[\"deadline\"]}}}\n\n" +
				"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n",
			wantItems:  2,
			wantFailed: true,
		},
		{
			name: "incomplete",
			buffered: `{"id":"resp_1","model":"m","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[` +
				`{"type":"web_search_call","id":"ws_1","status":"incomplete","action":{"type":"search","queries":["deadline"]}}]}`,
			streamed: "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"web_search_call\",\"id\":\"ws_1\",\"status\":\"incomplete\",\"action\":{\"type\":\"search\",\"queries\":[\"deadline\"]}}}\n\n" +
				"event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"},\"output\":[]}}\n\n",
			wantItems: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buffered, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, []byte(test.buffered), "ex_buffered", nil, true)
			if err != nil {
				t.Fatal(err)
			}
			streamed := decodeResponseStream(
				canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(test.streamed))},
				"ex_streamed", nil, true,
			)
			for name, reader := range map[string]canonical.ResponseStream{"buffered": buffered, "streamed": streamed} {
				closed, err := canonical.ReadClosedEnvelope(
					context.Background(),
					canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("resp_" + name)}),
					canonical.EnvResponse,
				)
				if err != nil {
					t.Fatalf("%s envelope: %v", name, err)
				}
				response, err := closed.ProjectResponse()
				if err != nil {
					t.Fatalf("%s response: %v", name, err)
				}
				items := response.Items()
				if len(items) != test.wantItems || items[0].Kind() != canonical.ItemKindToolCall {
					t.Fatalf("%s items = %#v", name, items)
				}
				if test.wantFailed {
					assertResponsesFailedWebSearchItems(t, items)
				}
			}
		})
	}
}
