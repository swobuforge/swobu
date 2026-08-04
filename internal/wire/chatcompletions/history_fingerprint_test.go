package chatcompletions

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestHistoryFingerprintRoundTrip(t *testing.T) {
	first := decodeChatFingerprintRequest(t, `{"model":"m","messages":[{"role":"user","content":"hello"}]}`)
	if first.RequestFingerprint.Scheme() != fingerprintScheme {
		t.Fatalf("request scheme = %q, want %q", first.RequestFingerprint.Scheme(), fingerprintScheme)
	}
	if first.RebasedRequest != nil {
		t.Fatalf("first rebased request = %#v, want nil", first.RebasedRequest)
	}
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{
		canonicaltest.Message(t, canonical.MessageRoleAssistant, "hi"),
	}, canonical.Completed("stop"))
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatalf("encode first response: %v", err)
	}
	if encoded.ResponseFingerprint == nil {
		t.Fatal("buffered response fingerprint is nil")
	}
	if encoded.ResponseFingerprint.Scheme() != fingerprintScheme {
		t.Fatalf("response scheme = %q, want %q", encoded.ResponseFingerprint.Scheme(), fingerprintScheme)
	}
	wantPrevious, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
	if err != nil {
		t.Fatalf("advance first checkpoint: %v", err)
	}

	second := decodeChatFingerprintRequest(t, `{
		"model":"different","temperature":0.9,
		"messages":[
			{"role":"user","content":"hello"},
			{"role":"assistant","content":"hi"},
			{"role":"user","content":"again"}
		]}`)
	if second.RebasedRequest == nil || second.RebasedRequest.Previous != wantPrevious {
		t.Fatalf("second rebased request = %#v, want history %#v", second.RebasedRequest, wantPrevious)
	}
	if len(second.RebasedRequest.Request.Items()) != 1 || second.RebasedRequest.Request.Model() != "different" {
		t.Fatalf("rebased invocation = %#v, want current item and invocation roots", second.RebasedRequest.Request)
	}
	changed := decodeChatFingerprintRequest(t, `{"model":"m","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"changed"},{"role":"user","content":"again"}]}`)
	if changed.RebasedRequest == nil || changed.RebasedRequest.Previous == wantPrevious {
		t.Fatal("changed historical response did not change predecessor")
	}
	changedCurrent := decodeChatFingerprintRequest(t, `{"model":"m","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"different"}]}`)
	if changedCurrent.RequestFingerprint == second.RequestFingerprint {
		t.Fatal("changed current input did not change request fingerprint")
	}
}

func TestHistoryFingerprintExcludesCurrentLeadingContext(t *testing.T) {
	first := decodeChatFingerprintRequest(t, `{
		"model":"m",
		"tools":[{"type":"function","function":{"name":"first_tool","parameters":{"type":"object"}}}],
		"messages":[
			{"role":"system","content":"system A"},
			{"role":"developer","content":"developer A"},
			{"role":"user","content":"hello"}
		]}`)
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{
		canonicaltest.Message(t, canonical.MessageRoleAssistant, "hi"),
	}, canonical.Completed("stop"))
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	wantPrevious, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	second := decodeChatFingerprintRequest(t, `{
		"model":"different",
		"tools":[{"type":"function","function":{"name":"second_tool","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}],
		"messages":[
			{"role":"system","content":"system B"},
			{"role":"developer","content":"developer B"},
			{"role":"user","content":"hello"},
			{"role":"assistant","content":"hi"},
			{"role":"user","content":"again"}
		]}`)
	if second.RebasedRequest == nil || second.RebasedRequest.Previous != wantPrevious {
		t.Fatalf("changed context predecessor = %#v, want %#v", second.RebasedRequest, wantPrevious)
	}
	items := second.RebasedRequest.Request.Items()
	if len(items) < 4 || canonicaltest.DirectiveText(items) == "" || len(canonicaltest.Tools(second.RebasedRequest.Request)) != 1 {
		t.Fatalf("rebased current context was not retained: %#v", second.RebasedRequest.Request)
	}
	withoutContext := decodeChatFingerprintRequest(t, `{
		"model":"different",
		"messages":[
			{"role":"user","content":"hello"},
			{"role":"assistant","content":"hi"},
			{"role":"user","content":"again"}
		]}`)
	if withoutContext.RebasedRequest == nil || withoutContext.RebasedRequest.Previous != wantPrevious {
		t.Fatalf("removed context predecessor = %#v, want %#v", withoutContext.RebasedRequest, wantPrevious)
	}
	mutated := decodeChatFingerprintRequest(t, `{
		"model":"m",
		"messages":[
			{"role":"system","content":"system B"},
			{"role":"user","content":"changed"},
			{"role":"assistant","content":"hi"},
			{"role":"user","content":"again"}
		]}`)
	if mutated.RebasedRequest == nil || mutated.RebasedRequest.Previous == wantPrevious {
		t.Fatal("historical conversation mutation retained predecessor")
	}
}

func TestExplicitPredecessorFingerprintsEverySuppliedMessage(t *testing.T) {
	raw := `{"model":"m","previous_response_id":"swobu_parent","messages":[{"role":"user","content":"A"},{"role":"assistant","content":"B"},{"role":"user","content":"C"}]}`
	decoded := decodeChatFingerprintRequest(t, raw)
	previous, ok := decoded.Request.PreviousResponse()
	if !ok || previous.SwobuID != "swobu_parent" {
		t.Fatalf("canonical predecessor = %#v, want swobu_parent", previous)
	}
	if decoded.RebasedRequest != nil {
		t.Fatalf("explicit rebased request = %#v, want nil", decoded.RebasedRequest)
	}
	var dto chatCompletionsRequestDTO
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		t.Fatal(err)
	}
	wantRequest, err := fingerprintChatCompletionsRequest(dto.Messages)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.RequestFingerprint != wantRequest {
		t.Fatalf("explicit request fingerprint = %#v, want all-message leaf %#v", decoded.RequestFingerprint, wantRequest)
	}
	lastOnly, err := fingerprintChatCompletionsRequest(dto.Messages[len(dto.Messages)-1:])
	if err != nil {
		t.Fatal(err)
	}
	response, err := fingerprintChatCompletionsResponseMessages([]chatCompletionsMessageDTO{{Role: "assistant", Content: json.RawMessage(`"next"`)}})
	if err != nil {
		t.Fatal(err)
	}
	base, err := historyfingerprint.Advance(nil, lastOnly, response)
	if err != nil {
		t.Fatal(err)
	}
	child, err := historyfingerprint.Advance(&base, decoded.RequestFingerprint, response)
	if err != nil {
		t.Fatal(err)
	}
	wantChild, err := historyfingerprint.Advance(&base, wantRequest, response)
	if err != nil {
		t.Fatal(err)
	}
	wrongChild, err := historyfingerprint.Advance(&base, lastOnly, response)
	if err != nil {
		t.Fatal(err)
	}
	if child != wantChild || child == wrongChild {
		t.Fatalf("explicit child = %#v, want full-input child %#v, last-only child %#v", child, wantChild, wrongChild)
	}
}

func TestBufferedAndStreamingResponseFingerprintsConverge(t *testing.T) {
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{
		canonicaltest.Message(t, canonical.MessageRoleAssistant, "hi"),
	}, canonical.Completed("stop"))
	buffered, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	events := canonical.SynthesizeResponseEnvelopeEvents("ex", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage())
	streamed, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, canonical.NewSliceEventReader(events), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(streamed.Stream.Body); err != nil {
		t.Fatal(err)
	}
	snapshot := streamed.Completion.Snapshot()
	if snapshot.State != wire.CompletionCompleted || snapshot.ResponseFingerprint == nil || buffered.ResponseFingerprint == nil || *snapshot.ResponseFingerprint != *buffered.ResponseFingerprint {
		t.Fatalf("stream completion = %#v, want buffered fingerprint %#v", snapshot, buffered.ResponseFingerprint)
	}

	incomplete, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, canonical.NewSliceEventReader(events[:len(events)-1]), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(incomplete.Stream.Body)
	if got := incomplete.Completion.Snapshot(); got.State != wire.CompletionFailed || got.ResponseFingerprint != nil {
		t.Fatalf("incomplete completion = %#v", got)
	}
}

func TestBufferedAndStreamingToolResponseFingerprintsConverge(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search")
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{call}, canonical.Completed("tool_calls"))
	buffered, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	events := canonical.SynthesizeResponseEnvelopeEvents("ex", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage())
	streamed, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, canonical.NewSliceEventReader(events), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(streamed.Stream.Body); err != nil {
		t.Fatal(err)
	}
	got := streamed.Completion.Snapshot()
	if got.State != wire.CompletionCompleted || got.ResponseFingerprint == nil || buffered.ResponseFingerprint == nil || *got.ResponseFingerprint != *buffered.ResponseFingerprint {
		t.Fatalf("tool stream completion = %#v, want %#v", got, buffered.ResponseFingerprint)
	}
}

func TestBufferedAndStreamingCustomToolResponseFingerprintsConverge(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "shell")
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewTextToolInput("echo exact"))
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{call}, canonical.Completed("tool_calls"))
	buffered, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	events := canonical.SynthesizeResponseEnvelopeEvents("ex", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage())
	streamed, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, canonical.NewSliceEventReader(events), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(streamed.Stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"type":"custom"`) || !strings.Contains(string(body), `"input":"echo exact"`) {
		t.Fatalf("custom stream = %s", body)
	}
	got := streamed.Completion.Snapshot()
	if got.State != wire.CompletionCompleted || got.ResponseFingerprint == nil || buffered.ResponseFingerprint == nil || *got.ResponseFingerprint != *buffered.ResponseFingerprint {
		t.Fatalf("custom stream completion = %#v, want %#v", got, buffered.ResponseFingerprint)
	}
}

func TestTerminalAssistantPrefillIsCurrentContribution(t *testing.T) {
	decoded := decodeChatFingerprintRequest(t, `{"model":"m","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"prefill"}]}`)
	if decoded.RebasedRequest != nil {
		t.Fatalf("terminal assistant prefill produced history: %#v", decoded.RebasedRequest)
	}
}

func TestChatCompletionsHistoryPartitionsToolLoopAndPreservesCallIDs(t *testing.T) {
	base := `{
		"model":"m",
		"tools":[{"type":"function","function":{"name":"search","parameters":{"type":"object"}}}],
		"messages":[
			{"role":"user","content":"start"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{\"q\":\"one\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"result"},
			{"role":"assistant","content":"done"},
			{"role":"user","content":"next"}
		]}`
	decoded := decodeChatFingerprintRequest(t, base)
	if decoded.RebasedRequest == nil || len(decoded.RebasedRequest.Request.Items()) != 2 {
		t.Fatalf("tool-loop rebased request = %#v, want request declarations and current user contribution", decoded.RebasedRequest)
	}
	changed := decodeChatFingerprintRequest(t, strings.ReplaceAll(base, "call_1", "call_changed"))
	if changed.RebasedRequest == nil || changed.RebasedRequest.Previous == decoded.RebasedRequest.Previous {
		t.Fatal("changing tool relationship ids did not change completed history")
	}
}

func TestChatCompletionsHistoryResumesAtCurrentToolResult(t *testing.T) {
	first := decodeChatFingerprintRequest(t, `{"model":"m","messages":[{"role":"user","content":"start"}]}`)
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search")
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{call}, canonical.Completed("tool_calls"))
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	wantPrevious, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	second := decodeChatFingerprintRequest(t, `{
		"model":"m",
		"tools":[{"type":"function","function":{"name":"search","parameters":{"type":"object"}}}],
		"messages":[
			{"role":"user","content":"start"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"search","arguments":"{\"q\":\"one\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"result"}
		]}`)
	assertCurrentChatToolResult(t, second, wantPrevious)
	direct := decodeChatFingerprintRequest(t, `{
		"model":"m","previous_response_id":"swobu_1",
		"messages":[{"role":"tool","tool_call_id":"call_1","content":"result"}]
	}`)
	if second.RequestFingerprint != direct.RequestFingerprint {
		t.Fatal("rebased tool-result fingerprint differs from the same direct contribution")
	}
}

func TestEncodedDocumentAppendAndReconstructLaw(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search")
	toolCall := func(t *testing.T) canonical.CanonicalItem {
		return canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
	}
	tests := []struct {
		name        string
		items       func(*testing.T) []canonical.CanonicalItem
		finish      string
		contentNull bool
	}{
		{name: "assistant text", items: func(t *testing.T) []canonical.CanonicalItem {
			return []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleAssistant, "hi")}
		}, finish: "stop"},
		{name: "tool call", items: func(t *testing.T) []canonical.CanonicalItem {
			return []canonical.CanonicalItem{toolCall(t)}
		}, finish: "tool_calls", contentNull: true},
		{name: "mixed text and tool call", items: func(t *testing.T) []canonical.CanonicalItem {
			return []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleAssistant, "checking"), toolCall(t)}
		}, finish: "tool_calls"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first := decodeChatFingerprintRequest(t, `{"model":"m","tools":[{"type":"function","function":{"name":"search","parameters":{"type":"object"}}}],"messages":[{"role":"user","content":"start"}]}`)
			response := canonicaltest.Response(t, "swobu_1", "m", test.items(t), canonical.Completed(test.finish))
			encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
			if err != nil {
				t.Fatal(err)
			}
			want, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Choices []struct {
					Message json.RawMessage `json:"message"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(encoded.Document.RawBytes(), &document); err != nil || len(document.Choices) != 1 {
				t.Fatalf("encoded document = (%s, %v)", encoded.Document.RawBytes(), err)
			}
			if test.contentNull {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(document.Choices[0].Message, &fields); err != nil {
					t.Fatal(err)
				}
				if content, ok := fields["content"]; !ok || string(content) != "null" {
					t.Fatalf("tool-only encoded content = %s, want explicit null", content)
				}
			}
			messages := []json.RawMessage{
				json.RawMessage(`{"role":"user","content":"start"}`),
				document.Choices[0].Message,
				json.RawMessage(`{"role":"user","content":"again"}`),
			}
			nextRaw, err := json.Marshal(struct {
				Model    string            `json:"model"`
				Tools    json.RawMessage   `json:"tools"`
				Messages []json.RawMessage `json:"messages"`
			}{Model: "m", Tools: json.RawMessage(`[{"type":"function","function":{"name":"search","parameters":{"type":"object"}}}]`), Messages: messages})
			if err != nil {
				t.Fatal(err)
			}
			reconstructed := decodeChatFingerprintRequest(t, string(nextRaw))
			if reconstructed.RebasedRequest == nil || reconstructed.RebasedRequest.Previous != want {
				t.Fatalf("encoded append predecessor = %#v, want %#v\n%s", reconstructed.RebasedRequest, want, nextRaw)
			}
		})
	}
}

func assertCurrentChatToolResult(t *testing.T, decoded wire.ClientRequestResult, previous historyfingerprint.History) {
	t.Helper()
	if decoded.RebasedRequest == nil || decoded.RebasedRequest.Previous != previous {
		t.Fatalf("rebased predecessor = %#v, want %#v", decoded.RebasedRequest, previous)
	}
	items := decoded.RebasedRequest.Request.Items()
	if len(items) != 2 || items[0].Kind() != canonical.ItemKindToolDeclarations || items[1].Kind() != canonical.ItemKindToolResult {
		t.Fatalf("rebased items = %#v, want request declarations and current tool result", items)
	}
	result, _ := items[1].ToolResult()
	if result.CallID().String() != "call_1" {
		t.Fatalf("tool result call ID = %q", result.CallID().String())
	}
}

func TestChatCompletionsHistoryPreservesMultimodalContent(t *testing.T) {
	base := `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"see"},{"type":"image_url","image_url":{"url":"https://example.test/one.png"}}]},{"role":"assistant","content":"seen"},{"role":"user","content":"next"}]}`
	decoded := decodeChatFingerprintRequest(t, base)
	changed := decodeChatFingerprintRequest(t, strings.Replace(base, "one.png", "two.png", 1))
	if decoded.RebasedRequest == nil || changed.RebasedRequest == nil || decoded.RebasedRequest.Previous == changed.RebasedRequest.Previous {
		t.Fatal("changing historical image content did not change completed history")
	}
}

func decodeChatFingerprintRequest(t *testing.T, raw string) wire.ClientRequestResult {
	t.Helper()
	result, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(raw), carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	return result.Request
}
