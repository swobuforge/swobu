package messages

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
	first := decodeMessagesFingerprintRequest(t, `{"model":"m","system":"one","messages":[{"role":"user","content":"hello"}]}`)
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleAssistant, "hi")}, "end_turn")
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	if encoded.ResponseFingerprint == nil {
		t.Fatal("buffered response fingerprint is nil")
	}
	wantPrevious, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	second := decodeMessagesFingerprintRequest(t, `{"model":"other","system":"changed","temperature":1,"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":[{"type":"text","text":"hi"}]},{"role":"user","content":"again"}]}`)
	if second.RebasedRequest == nil || second.RebasedRequest.Previous != wantPrevious {
		t.Fatalf("rebased request = %#v, want history %#v", second.RebasedRequest, wantPrevious)
	}
	if len(second.RebasedRequest.Request.Items()) != 1 || second.RebasedRequest.Request.Model() != "other" || second.RebasedRequest.Request.Instructions().IsEmpty() {
		t.Fatalf("rebased invocation did not preserve current fields: %#v", second.RebasedRequest.Request)
	}
	changed := decodeMessagesFingerprintRequest(t, `{"model":"m","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":[{"type":"text","text":"changed"}]},{"role":"user","content":"again"}]}`)
	if changed.RebasedRequest == nil || changed.RebasedRequest.Previous == wantPrevious {
		t.Fatal("changed historical content did not change predecessor")
	}
}

func TestExplicitPredecessorFingerprintsEverySuppliedMessage(t *testing.T) {
	raw := `{"model":"m","previous_response_id":"swobu_parent","messages":[{"role":"user","content":"A"},{"role":"assistant","content":"B"},{"role":"user","content":"C"}]}`
	decoded := decodeMessagesFingerprintRequest(t, raw)
	previous, ok := decoded.Request.PreviousResponse()
	if !ok || previous.SwobuID != "swobu_parent" {
		t.Fatalf("canonical predecessor = %#v, want swobu_parent", previous)
	}
	if decoded.RebasedRequest != nil {
		t.Fatalf("explicit rebased request = %#v, want nil", decoded.RebasedRequest)
	}
	var dto messagesRequestDTO
	if err := json.Unmarshal([]byte(raw), &dto); err != nil {
		t.Fatal(err)
	}
	wantRequest, err := fingerprintMessagesRequest(dto.Messages)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.RequestFingerprint != wantRequest {
		t.Fatalf("explicit request fingerprint = %#v, want all-message leaf %#v", decoded.RequestFingerprint, wantRequest)
	}
	lastOnly, err := fingerprintMessagesRequest(dto.Messages[len(dto.Messages)-1:])
	if err != nil {
		t.Fatal(err)
	}
	response, err := fingerprintMessagesResponseValue(messagesMessageDTO{Role: "assistant", Content: json.RawMessage(`"next"`)})
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
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleAssistant, "hi")}, "end_turn")
	buffered, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	events := canonical.SynthesizeResponseEnvelopeEvents("ex", response.Response(), response.Model(), response.Items(), response.CompletionReason(), response.Usage())
	streamed, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, canonical.NewSliceEventReader(events), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(streamed.Stream.Body); err != nil {
		t.Fatal(err)
	}
	snapshot := streamed.Completion.Snapshot()
	if snapshot.State != wire.CompletionCompleted || snapshot.ResponseFingerprint == nil || buffered.ResponseFingerprint == nil || *snapshot.ResponseFingerprint != *buffered.ResponseFingerprint {
		t.Fatalf("stream completion = %#v, want %#v", snapshot, buffered.ResponseFingerprint)
	}
}

func TestBufferedAndStreamingToolResponseFingerprintsConverge(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search")
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{call}, "tool_use")
	buffered, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	events := canonical.SynthesizeResponseEnvelopeEvents("ex", response.Response(), response.Model(), response.Items(), response.CompletionReason(), response.Usage())
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

func TestTerminalAssistantPrefillIsCurrentContribution(t *testing.T) {
	decoded := decodeMessagesFingerprintRequest(t, `{"model":"m","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"prefill"}]}`)
	if decoded.RebasedRequest != nil {
		t.Fatalf("terminal assistant prefill produced history: %#v", decoded.RebasedRequest)
	}
}

func TestMessagesHistoryPartitionsToolLoopAndPreservesCallIDs(t *testing.T) {
	base := `{
		"model":"m",
		"tools":[{"name":"search","input_schema":{"type":"object"}}],
		"messages":[
			{"role":"user","content":"start"},
			{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"search","input":{"q":"one"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"result"}]},
			{"role":"assistant","content":[{"type":"text","text":"done"}]},
			{"role":"user","content":"next"}
		]}`
	decoded := decodeMessagesFingerprintRequest(t, base)
	if decoded.RebasedRequest == nil || len(decoded.RebasedRequest.Request.Items()) != 1 {
		t.Fatalf("tool-loop rebased request = %#v, want current user contribution", decoded.RebasedRequest)
	}
	changed := decodeMessagesFingerprintRequest(t, strings.ReplaceAll(base, "call_1", "call_changed"))
	if changed.RebasedRequest == nil || changed.RebasedRequest.Previous == decoded.RebasedRequest.Previous {
		t.Fatal("changing tool relationship ids did not change completed history")
	}
}

func TestMessagesHistoryResumesAtCurrentToolResult(t *testing.T) {
	first := decodeMessagesFingerprintRequest(t, `{"model":"m","messages":[{"role":"user","content":"start"}]}`)
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search")
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{call}, "tool_use")
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	wantPrevious, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	second := decodeMessagesFingerprintRequest(t, `{
		"model":"m",
		"tools":[{"name":"search","input_schema":{"type":"object"}}],
		"messages":[
			{"role":"user","content":"start"},
			{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"search","input":{"q":"one"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"result"}]}
		]}`)
	assertCurrentMessagesToolResult(t, second, wantPrevious)
	direct := decodeMessagesFingerprintRequest(t, `{
		"model":"m","previous_response_id":"swobu_1",
		"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"result"}]}]
	}`)
	if second.RequestFingerprint != direct.RequestFingerprint {
		t.Fatal("rebased tool-result fingerprint differs from the same direct contribution")
	}
}

func TestEncodedDocumentAppendAndReconstructLaw(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search")
	tests := []struct {
		name   string
		items  func(*testing.T) []canonical.CanonicalItem
		finish string
	}{
		{name: "assistant text", items: func(t *testing.T) []canonical.CanonicalItem {
			return []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleAssistant, "hi")}
		}, finish: "end_turn"},
		{name: "tool use", items: func(t *testing.T) []canonical.CanonicalItem {
			call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
			return []canonical.CanonicalItem{call}
		}, finish: "tool_use"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first := decodeMessagesFingerprintRequest(t, `{"model":"m","tools":[{"name":"search","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"start"}]}`)
			response := canonicaltest.Response(t, "swobu_1", "m", test.items(t), test.finish)
			encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
			if err != nil {
				t.Fatal(err)
			}
			want, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Content json.RawMessage `json:"content"`
			}
			if err := json.Unmarshal(encoded.Document.RawBytes(), &document); err != nil {
				t.Fatal(err)
			}
			assistant, err := json.Marshal(struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			}{Role: "assistant", Content: document.Content})
			if err != nil {
				t.Fatal(err)
			}
			messages := []json.RawMessage{
				json.RawMessage(`{"role":"user","content":"start"}`), assistant,
				json.RawMessage(`{"role":"user","content":"again"}`),
			}
			nextRaw, err := json.Marshal(struct {
				Model    string            `json:"model"`
				Tools    json.RawMessage   `json:"tools"`
				Messages []json.RawMessage `json:"messages"`
			}{Model: "m", Tools: json.RawMessage(`[{"name":"search","input_schema":{"type":"object"}}]`), Messages: messages})
			if err != nil {
				t.Fatal(err)
			}
			reconstructed := decodeMessagesFingerprintRequest(t, string(nextRaw))
			if reconstructed.RebasedRequest == nil || reconstructed.RebasedRequest.Previous != want {
				t.Fatalf("encoded append predecessor = %#v, want %#v\n%s", reconstructed.RebasedRequest, want, nextRaw)
			}
		})
	}
}

func assertCurrentMessagesToolResult(t *testing.T, decoded wire.ClientRequestResult, previous historyfingerprint.History) {
	t.Helper()
	if decoded.RebasedRequest == nil || decoded.RebasedRequest.Previous != previous {
		t.Fatalf("rebased predecessor = %#v, want %#v", decoded.RebasedRequest, previous)
	}
	items := decoded.RebasedRequest.Request.Items()
	if len(items) != 1 || items[0].Kind() != canonical.ItemKindToolResult {
		t.Fatalf("rebased items = %#v, want one current tool result", items)
	}
	result, _ := items[0].ToolResult()
	if result.CallID().String() != "call_1" {
		t.Fatalf("tool result call ID = %q", result.CallID().String())
	}
}

func TestMessagesHistoryPreservesMultimodalContent(t *testing.T) {
	base := `{"model":"m","messages":[{"role":"user","content":[{"type":"text","text":"see"},{"type":"image","source":{"type":"url","url":"https://example.test/one.png"}}]},{"role":"assistant","content":"seen"},{"role":"user","content":"next"}]}`
	decoded := decodeMessagesFingerprintRequest(t, base)
	changed := decodeMessagesFingerprintRequest(t, strings.Replace(base, "one.png", "two.png", 1))
	if decoded.RebasedRequest == nil || changed.RebasedRequest == nil || decoded.RebasedRequest.Previous == changed.RebasedRequest.Previous {
		t.Fatal("changing historical image content did not change completed history")
	}
}

func decodeMessagesFingerprintRequest(t *testing.T, raw string) wire.ClientRequestResult {
	t.Helper()
	result, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(protocolkind.Messages, "application/json", nil, []byte(raw), carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	return result.Request
}
