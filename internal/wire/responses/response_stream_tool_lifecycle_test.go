package responses

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestDecodeResponseStream_PreservesMultiDeltaMessageAfterExpandedWebSearch(t *testing.T) {
	t.Parallel()

	var providerWire strings.Builder
	providerWire.WriteString("event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_provider\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n")
	for index := range 4 {
		providerWire.WriteString(fmt.Sprintf("event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":%d,\"item\":{\"id\":\"ws_%d\",\"type\":\"web_search_call\",\"status\":\"in_progress\"}}\n\n", index, index))
		providerWire.WriteString(fmt.Sprintf("event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":{\"id\":\"ws_%d\",\"type\":\"web_search_call\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"queries\":[\"deadline\"],\"sources\":[]}}}\n\n", index, index))
	}
	providerWire.WriteString("event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":4,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"in_progress\",\"role\":\"assistant\",\"content\":[]}}\n\n")
	providerWire.WriteString("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":4,\"content_index\":0,\"delta\":\"first \"}\n\n")
	providerWire.WriteString("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":4,\"content_index\":0,\"delta\":\"second\"}\n\n")
	providerWire.WriteString("event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":4,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"first second\",\"annotations\":[]}]}}\n\n")
	providerWire.WriteString("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_provider\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n")

	decoded := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(providerWire.String()))}, "exchange", nil, true)
	bound := canonical.NewBoundResponseIdentityStream(decoded, canonical.ResponseBinding{SwobuID: "resp_client"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	items := response.Items()
	if len(items) != 9 {
		t.Fatalf("output items len=%d, want 9", len(items))
	}
	message, ok := items[8].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("message item = %#v", items[8])
	}
	text, ok := message.Content()[0].Text()
	if !ok || text.Text() != "first second" {
		t.Fatalf("message text = %#v, want first second", message.Content())
	}
}

func TestDecodeResponseStream_DoesNotReopenAnonymousToolCallOnSecondDoneFrame(t *testing.T) {
	t.Parallel()

	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"store\":true,\"output\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"call_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"Bash\"}}\n\n" +
		"event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"call_1\",\"call_id\":\"call_1\",\"name\":\"Bash\",\"delta\":\"{\\\"command\\\":\\\"cat fixture\\\"}\"}\n\n" +
		"event: response.function_call_arguments.done\ndata: {\"type\":\"response.function_call_arguments.done\",\"output_index\":0,\"item_id\":\"call_1\",\"call_id\":\"call_1\",\"name\":\"Bash\",\"arguments\":\"{\\\"command\\\":\\\"cat fixture\\\"}\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"call_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"arguments\":\"{\\\"command\\\":\\\"cat fixture\\\"}\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"

	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "Bash"), "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]()))}})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	reader := decodeResponseStream(
		request,
		names,
		carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex_stream_tool_lifecycle",
		nil, true,
	)

	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	out, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	items := out.Items()
	if len(items) != 1 {
		t.Fatalf("output items len=%d, want 1", len(items))
	}
	item := items[0]
	if item.Kind() != canonical.ItemKindToolCall {
		t.Fatalf("output item kind=%s, want %s", item.Kind(), canonical.ItemKindToolCall)
	}
	toolUse, _ := item.ToolCall()
	if toolUse.CallID().String() != "call_1" {
		t.Fatalf("tool use id=%q, want call_1", toolUse.CallID().String())
	}
}

func TestDecodeResponseStream_IgnoresDuplicateResponseCreated(t *testing.T) {
	t.Parallel()

	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"store\":true,\"output\":[]}}\n\n" +
		"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_2\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_2\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"

	reader := decodeResponseStream(
		canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex_stream_duplicate_created",
		nil, true,
	)

	starts := 0
	for {
		ev, err := reader.Next(context.Background())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next returned error: %v", err)
		}
		if ev.Kind == canonical.EventResponseIdentity {
			identity, ok := ev.Payload.(canonical.ResponseIdentityPayload)
			if !ok || identity.Response.Responses == nil || identity.Response.Responses.ProviderResponseID != "resp_1" {
				t.Fatalf("response identity=%#v", ev.Payload)
			}
		}
		if ev.Kind != canonical.EventEnvelopeStart {
			continue
		}
		payload, ok := ev.Payload.(canonical.EnvelopeStartPayload)
		if !ok {
			t.Fatalf("start payload type = %T, want EnvelopeStartPayload", ev.Payload)
		}
		if payload.Kind != canonical.EnvResponse {
			continue
		}
		starts++
		if starts > 1 {
			t.Fatalf("duplicate response.created emitted %d response starts", starts)
		}
	}
	if starts != 1 {
		t.Fatalf("response start count = %d, want 1", starts)
	}
}

func TestEncodeResponseStreamPreservesWebSearchLifecycleKind(t *testing.T) {
	callID, _ := canonical.NewToolCallID("search_original")
	input, err := canonical.NewWebSearchToolInput(canonical.WebSearchCall{
		Action: canonical.WebSearchActionSearch, Queries: []string{"Dmytrii Shchadei"},
	})
	if err != nil {
		t.Fatal(err)
	}
	call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	webURL, _ := canonical.NewWebURL("https://example.com/source")
	source, _ := canonical.NewWebSource(webURL, canonical.Specify("Source"))
	resultValue, _ := canonical.NewWebSearchResult([]canonical.WebSource{source})
	result, _ := canonical.NewWebSearchResultItem(callID, resultValue)
	part, _ := canonical.NewCitedTextMessagePart("found", []canonical.WebCitation{{Source: source}})
	message, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{part})
	inputTokens, outputTokens := 2, 3
	usage, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: &inputTokens, OutputTokens: &outputTokens})
	response, _ := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_search")},
		"model", []canonical.CanonicalItem{call, result, message}, canonical.Completed("stop"), usage,
	)
	buffered, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	bufferedReader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, buffered.Document.RawBytes(), "buffered", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	assertResponsesWebSearchSemantics(t, bufferedReader, callID)

	events := canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents(
		"exchange", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage(),
	))
	encoded, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, events, delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(encoded.Stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	if !strings.Contains(wire, `"sequence_number":0`) {
		t.Fatalf("Responses SSE omitted its initial sequence number: %s", wire)
	}
	for _, event := range []string{"response.created", "response.output_item.added", "response.output_item.done", "response.completed"} {
		if !strings.Contains(wire, "event: "+event+"\n") {
			t.Fatalf("Responses SSE omitted event name %q: %s", event, wire)
		}
	}
	if !strings.Contains(wire, `"type":"web_search_call"`) || !strings.Contains(wire, `"id":"search_original"`) {
		t.Fatalf("web-search lifecycle missing: %s", wire)
	}
	if !strings.Contains(wire, `"action":{"type":"search","query":"Dmytrii Shchadei","sources":[{"type":"url","url":"https://example.com/source","title":"Source"}]}`) {
		t.Fatalf("completed web-search action missing: %s", wire)
	}
	if !strings.Contains(wire, `"annotations":[{"type":"url_citation","url":"https://example.com/source","title":"Source"`) ||
		!strings.Contains(wire, `"usage":{"input_tokens":2,"output_tokens":3`) {
		t.Fatalf("stream lost citations or usage: %s", wire)
	}
	if strings.Contains(wire, `"type":"function_call"`) || strings.Contains(wire, "function_call_arguments") {
		t.Fatalf("web search was projected as a function call: %s", wire)
	}

	decoded := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(wire))}, "replay", nil, true)
	assertResponsesWebSearchSemantics(t, decoded, callID)
}

func assertResponsesWebSearchSemantics(t *testing.T, reader canonical.ResponseStream, callID canonical.ToolCallID) {
	t.Helper()
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "replayed"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	items := replayed.Items()
	if len(items) != 3 || items[0].Kind() != canonical.ItemKindToolCall || items[1].Kind() != canonical.ItemKindToolResult {
		t.Fatalf("replayed items = %#v", items)
	}
	replayedCall, _ := items[0].ToolCall()
	if replayedCall.CallID() != callID || replayedCall.Tool() != canonical.WebSearchToolKey() {
		t.Fatalf("replayed call = %#v", replayedCall)
	}
	replayedResult, _ := items[1].ToolResult()
	search, _ := replayedResult.WebSearch()
	if len(search.Sources()) != 1 || search.Sources()[0].URL.String() != "https://example.com/source" {
		t.Fatalf("replayed sources = %#v", search.Sources())
	}
	replayedMessage, _ := items[2].Message()
	if len(replayedMessage.Content()[0].Citations()) != 1 {
		t.Fatalf("replayed citations = %#v", replayedMessage.Content()[0].Citations())
	}
	if input, ok := replayed.Usage().InputTokens(); !ok || input != 2 {
		t.Fatalf("replayed input usage = (%d,%t)", input, ok)
	}
	if output, ok := replayed.Usage().OutputTokens(); !ok || output != 3 {
		t.Fatalf("replayed output usage = (%d,%t)", output, ok)
	}
}

func TestResponsesRoundTripCompletesSourceUndisclosedSearchBeforeAnswer(t *testing.T) {
	declaration := canonical.NewWebSearchDeclaration()
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	if err != nil {
		t.Fatal(err)
	}
	declarations, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarations}})
	providerWire := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_provider\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"ws_1\",\"type\":\"web_search_call\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"ws_1\",\"type\":\"web_search_call\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"queries\":[\"deadline\"],\"sources\":null}}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":1,\"content_index\":0,\"delta\":\"Answer\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"Answer\",\"annotations\":[]}]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_provider\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"
	decoded := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(providerWire))}, "exchange", nil, true)
	bound := canonical.NewBoundResponseIdentityStream(decoded, canonical.ResponseBinding{SwobuID: "resp_client"})
	validated := canonical.NewValidatedResponseStream(bound)
	encoded, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), request, validated, delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(encoded.Stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	searchDone := strings.Index(wire, `"type":"response.output_item.done","output_index":0,"item":{"type":"web_search_call"`)
	answerDelta := strings.Index(wire, `"type":"response.output_text.delta"`)
	if searchDone < 0 || answerDelta < 0 || searchDone > answerDelta {
		t.Fatalf("search completion must precede answer text: %s", wire)
	}
	completedIndex := strings.LastIndex(wire, `"type":"response.completed"`)
	if completedIndex < 0 {
		t.Fatalf("terminal completion missing: %s", wire)
	}
	completed := wire[completedIndex:]
	if strings.Index(completed, `"type":"web_search_call"`) > strings.Index(completed, `"type":"message"`) {
		t.Fatalf("terminal output order = %s", completed)
	}
}
