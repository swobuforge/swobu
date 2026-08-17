package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	first := decodeResponsesFingerprintRequest(t, `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	if first.RequestFingerprint.Scheme() != fingerprintScheme {
		t.Fatalf("request scheme = %q, want %q", first.RequestFingerprint.Scheme(), fingerprintScheme)
	}
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleAssistant, "hi")}, canonical.Completed("completed"))
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	if encoded.ResponseFingerprint == nil {
		t.Fatal("buffered response fingerprint is nil")
	}
	if encoded.ResponseFingerprint.Scheme() != fingerprintScheme {
		t.Fatalf("response scheme = %q, want %q", encoded.ResponseFingerprint.Scheme(), fingerprintScheme)
	}
	wantPrevious, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	second := decodeResponsesFingerprintRequest(t, `{
		"model":"other","instructions":"changed","temperature":1,
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hi"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}]}
		]}`)
	if second.RebasedRequest == nil || second.RebasedRequest.Previous != wantPrevious {
		t.Fatalf("rebased request = %#v, want history %#v", second.RebasedRequest, wantPrevious)
	}
	if len(second.RebasedRequest.Request.Items()) != 2 || second.RebasedRequest.Request.Model() != "other" || canonicaltest.DirectiveText(second.RebasedRequest.Request.Items()) == "" {
		t.Fatalf("rebased invocation did not preserve current fields: %#v", second.RebasedRequest.Request)
	}
	changed := decodeResponsesFingerprintRequest(t, `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"changed"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}]}]}`)
	if changed.RebasedRequest == nil || changed.RebasedRequest.Previous == wantPrevious {
		t.Fatal("changed historical output did not change predecessor")
	}
	explicit := decodeResponsesFingerprintRequest(t, `{"model":"m","previous_response_id":"swobu_1","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}]}]}`)
	if explicit.RebasedRequest != nil {
		t.Fatalf("explicit request returned implicit rebasing %#v", explicit.RebasedRequest)
	}
}

func TestHistoryFingerprint_IgnoresInvocationLocalMetadata(t *testing.T) {
	plainItems := []responsesHistoryItemDTO{{
		Type: "message", Role: "user",
		Content: json.RawMessage(`[{"type":"input_text","text":"hello"},{"type":"input_image","image_url":"https://example.test/one.png"}]`),
	}}
	controlledItems := []responsesHistoryItemDTO{{
		Type: "message", Role: "user",
		Content: json.RawMessage(`[{"type":"input_text","text":"hello","prompt_cache_breakpoint":{"ttl":"5m"}},{"type":"input_image","image_url":"https://example.test/one.png","prompt_cache_breakpoint":{"ttl":"5m"}}]`),
	}}

	plainRequest, err := fingerprintResponsesRequestValue(plainItems)
	if err != nil {
		t.Fatal(err)
	}
	controlledRequest, err := fingerprintResponsesRequestValue(controlledItems)
	if err != nil {
		t.Fatal(err)
	}
	if controlledRequest != plainRequest {
		t.Fatal("invocation-local content metadata changed Responses request history identity")
	}

	responseItems := []responsesHistoryItemDTO{{Type: "message", Role: "assistant", Content: json.RawMessage(`[{"type":"output_text","text":"hi"}]`)}}
	responseFingerprint, err := fingerprintResponsesResponseValue(responseItems)
	if err != nil {
		t.Fatal(err)
	}
	plainHistory, err := historyfingerprint.Advance(nil, plainRequest, responseFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	controlledHistory, err := historyfingerprint.Advance(nil, controlledRequest, responseFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if controlledHistory != plainHistory {
		t.Fatal("invocation-local content metadata changed completed Responses history identity")
	}

	mutations := map[string][]responsesHistoryItemDTO{
		"text":        {{Type: "message", Role: "user", Content: json.RawMessage(`[{"type":"input_text","text":"hello!"},{"type":"input_image","image_url":"https://example.test/one.png"}]`)}},
		"image":       {{Type: "message", Role: "user", Content: json.RawMessage(`[{"type":"input_text","text":"hello"},{"type":"input_image","image_url":"https://example.test/two.png"}]`)}},
		"tool result": {{Type: "function_call_output", CallID: "call_1", Output: json.RawMessage(`"changed"`)}},
	}
	toolResultBase, err := fingerprintResponsesRequestValue([]responsesHistoryItemDTO{{Type: "function_call_output", CallID: "call_1", Output: json.RawMessage(`"result"`)}})
	if err != nil {
		t.Fatal(err)
	}
	for name, items := range mutations {
		t.Run(name, func(t *testing.T) {
			fingerprint, err := fingerprintResponsesRequestValue(items)
			if err != nil {
				t.Fatal(err)
			}
			base := plainRequest
			if name == "tool result" {
				base = toolResultBase
			}
			if fingerprint == base {
				t.Fatalf("changed Responses %s retained request history identity", name)
			}
		})
	}
}

func TestToolSearchOutputHistoryPartitionUsesExecutionOwner(t *testing.T) {
	client := responsesHistoryItemDTO{Type: "tool_search_output", Execution: "client"}
	server := responsesHistoryItemDTO{Type: "tool_search_output", Execution: "server"}
	if isResponsesHistoryOutput(client) {
		t.Fatal("client-executed discovery output was classified as provider output")
	}
	if !isResponsesHistoryOutput(server) {
		t.Fatal("provider-executed discovery output was classified as client input")
	}
}

func TestScalarInputFingerprintMatchesItsFutureHistoryItem(t *testing.T) {
	first := decodeResponsesFingerprintRequest(t, `{"model":"m","input":"hello"}`)
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{
		canonicaltest.Message(t, canonical.MessageRoleAssistant, "hi"),
	}, canonical.Completed("completed"))
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	wantPrevious, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	second := decodeResponsesFingerprintRequest(t, `{
		"model":"m",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hi"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}]}
		]}`)
	if second.RebasedRequest == nil || second.RebasedRequest.Previous != wantPrevious {
		t.Fatalf("scalar predecessor = %#v, want %#v", second.RebasedRequest, wantPrevious)
	}
}

func TestRequestFingerprintAppendAndReconstructLaw(t *testing.T) {
	textResponse := func(t *testing.T) canonical.CanonicalResponse {
		return canonicaltest.Response(t, "swobu_text", "m", []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "hi"),
		}, canonical.Completed("completed"))
	}
	toolResponse := func(t *testing.T) canonical.CanonicalResponse {
		key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search")
		call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
		return canonicaltest.Response(t, "swobu_tool", "m", []canonical.CanonicalItem{call}, canonical.Completed("tool_calls"))
	}
	tests := []struct {
		name              string
		firstInput        string
		appendableRequest string
		response          func(*testing.T) canonical.CanonicalResponse
		nextContribution  string
	}{
		{
			name:       "scalar text normalizes to input message",
			firstInput: `"hello"`,
			appendableRequest: `[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
			]`,
			response:         textResponse,
			nextContribution: `[{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}]}]`,
		},
		{
			name:       "scalar escaping and unicode",
			firstInput: `"line\n雪"`,
			appendableRequest: `[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"line\n雪"}]}
			]`,
			response:         textResponse,
			nextContribution: `[{"type":"message","role":"user","content":"again"}]`,
		},
		{
			name:              "structured text item",
			firstInput:        `[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]`,
			appendableRequest: `[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]`,
			response:          textResponse,
			nextContribution:  `[{"type":"message","role":"user","content":"again"}]`,
		},
		{
			name:              "multimodal ordered content",
			firstInput:        `[{"type":"message","role":"user","content":[{"type":"input_text","text":"see"},{"type":"input_image","image_url":"https://example.test/image.png"}]}]`,
			appendableRequest: `[{"type":"message","role":"user","content":[{"type":"input_text","text":"see"},{"type":"input_image","image_url":"https://example.test/image.png"}]}]`,
			response:          textResponse,
			nextContribution:  `[{"type":"message","role":"user","content":"again"}]`,
		},
		{
			name: "multiple ordered request items",
			firstInput: `[
				{"type":"message","role":"user","content":"one"},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"two"}]}
			]`,
			appendableRequest: `[
				{"type":"message","role":"user","content":"one"},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"two"}]}
			]`,
			response:         textResponse,
			nextContribution: `[{"type":"message","role":"user","content":"again"}]`,
		},
		{
			name:              "tool response closes before current function output",
			firstInput:        `"start"`,
			appendableRequest: `[{"type":"message","role":"user","content":[{"type":"input_text","text":"start"}]}]`,
			response:          toolResponse,
			nextContribution:  `[{"type":"function_call_output","call_id":"call_1","output":"result"}]`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first := decodeResponsesFingerprintRequest(t, `{"model":"m","input":`+test.firstInput+`}`)
			encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, test.response(t))
			if err != nil {
				t.Fatal(err)
			}
			if encoded.ResponseFingerprint == nil {
				t.Fatal("encoded response fingerprint is nil")
			}
			want, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
			if err != nil {
				t.Fatal(err)
			}

			var responseDocument struct {
				Output []json.RawMessage `json:"output"`
			}
			if err := json.Unmarshal(encoded.Document.RawBytes(), &responseDocument); err != nil {
				t.Fatal(err)
			}
			var input []json.RawMessage
			if err := json.Unmarshal([]byte(test.appendableRequest), &input); err != nil {
				t.Fatal(err)
			}
			input = append(input, responseDocument.Output...)
			var next []json.RawMessage
			if err := json.Unmarshal([]byte(test.nextContribution), &next); err != nil {
				t.Fatal(err)
			}
			input = append(input, next...)
			nextRaw, err := json.Marshal(struct {
				Model string            `json:"model"`
				Input []json.RawMessage `json:"input"`
			}{Model: "m", Input: input})
			if err != nil {
				t.Fatal(err)
			}
			reconstructed := decodeResponsesFingerprintRequest(t, string(nextRaw))
			if reconstructed.RebasedRequest == nil || reconstructed.RebasedRequest.Previous != want {
				t.Fatalf("reconstructed predecessor = %#v, want %#v\nrequest: %s", reconstructed.RebasedRequest, want, nextRaw)
			}
		})
	}
}

func FuzzScalarInputAppendAndReconstructLaw(f *testing.F) {
	for _, seed := range []string{"hello", "", "  padded  ", "line\n雪", `quotes " and slash \\`, strings.Repeat("x", 4096)} {
		f.Add(seed)
	}
	response := canonicaltest.Response(f, "swobu_fuzz", "m", []canonical.CanonicalItem{
		canonicaltest.Message(f, canonical.MessageRoleAssistant, "hi"),
	}, canonical.Completed("completed"))
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		f.Fatal(err)
	}
	var responseDocument struct {
		Output []json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(encoded.Document.RawBytes(), &responseDocument); err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, scalar string) {
		scalarRaw, err := json.Marshal(scalar)
		if err != nil {
			t.Fatal(err)
		}
		firstRaw, err := json.Marshal(struct {
			Model string          `json:"model"`
			Input json.RawMessage `json:"input"`
		}{Model: "m", Input: scalarRaw})
		if err != nil {
			t.Fatal(err)
		}
		decodedFirst, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(protocolkind.Responses, "application/json", nil, firstRaw, carrier.Meta{}))
		if err != nil {
			t.Fatal(err)
		}
		first := decodedFirst.Request
		want, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
		if err != nil {
			t.Fatal(err)
		}
		content, err := json.Marshal([]map[string]string{{"type": "input_text", "text": scalar}})
		if err != nil {
			t.Fatal(err)
		}
		requestItem, err := json.Marshal(map[string]any{
			"type": "message", "role": "user", "content": json.RawMessage(content),
		})
		if err != nil {
			t.Fatal(err)
		}
		input := []json.RawMessage{requestItem}
		input = append(input, responseDocument.Output...)
		input = append(input, json.RawMessage(`{"type":"message","role":"user","content":"again"}`))
		nextRaw, err := json.Marshal(struct {
			Model string            `json:"model"`
			Input []json.RawMessage `json:"input"`
		}{Model: "m", Input: input})
		if err != nil {
			t.Fatal(err)
		}
		reconstructed := decodeResponsesFingerprintRequest(t, string(nextRaw))
		if reconstructed.RebasedRequest == nil || reconstructed.RebasedRequest.Previous != want {
			t.Fatalf("scalar %q reconstructed predecessor = %#v, want %#v", scalar, reconstructed.RebasedRequest, want)
		}
	})
}

func TestBufferedAndStreamingResponseFingerprintsConverge(t *testing.T) {
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleAssistant, "hi")}, canonical.Completed("completed"))
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
		t.Fatalf("stream completion = %#v, want %#v", snapshot, buffered.ResponseFingerprint)
	}
}

func TestBufferedAndStreamingToolResponseFingerprintsConvergeAcrossCarriers(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search")
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{call}, canonical.Completed("tool_calls"))
	buffered, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	for _, framing := range []delivery.Framing{delivery.FramingSSE, delivery.FramingWebSocket} {
		t.Run(string(framing), func(t *testing.T) {
			events := canonical.SynthesizeResponseEnvelopeEvents("ex", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage())
			if framing == delivery.FramingSSE {
				streamed, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, canonical.NewSliceEventReader(events), delivery.StreamingDelivery(framing))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := io.ReadAll(streamed.Stream.Body); err != nil {
					t.Fatal(err)
				}
				assertResponsesCompletionFingerprint(t, streamed.Completion, buffered.ResponseFingerprint)
				return
			}
			streamed, err := (ResponseStreamEncoder{}).EncodeResponseMessages(context.Background(), canonical.CanonicalRequest{}, canonical.NewSliceEventReader(events), delivery.StreamingDelivery(framing))
			if err != nil {
				t.Fatal(err)
			}
			for {
				_, err := streamed.Response.Messages.Next(context.Background())
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			assertResponsesCompletionFingerprint(t, streamed.Completion, buffered.ResponseFingerprint)
		})
	}
}

func TestForeignOpaqueReasoningDoesNotEnterResponsesOutputOrHistory(t *testing.T) {
	first := decodeResponsesFingerprintRequest(t, `{"model":"m","input":"start"}`)
	opaque, err := canonical.NewMessagesOpaqueThinking([]byte(`{"type":"thinking","thinking":"private","signature":"sig"}`))
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem(nil, opaque)
	if err != nil {
		t.Fatal(err)
	}
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search")
	call := canonicaltest.ToolCall(t, "call_1", key, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
	response := canonicaltest.Response(t, "swobu_1", "m", []canonical.CanonicalItem{reasoning, call}, canonical.Completed("tool_calls"))

	buffered, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(buffered.Document.RawBytes()), `"type":"reasoning"`) {
		t.Fatalf("buffered output exposed foreign opaque reasoning: %s", buffered.Document.RawBytes())
	}
	events := canonical.SynthesizeResponseEnvelopeEvents("ex", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage())
	streamed, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, canonical.NewSliceEventReader(events), delivery.StreamingDelivery(delivery.FramingSSE))
	if err != nil {
		t.Fatal(err)
	}
	streamBody, err := io.ReadAll(streamed.Stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(streamBody), `"type":"reasoning"`) {
		t.Fatalf("stream output exposed foreign opaque reasoning: %s", streamBody)
	}
	assertResponsesCompletionFingerprint(t, streamed.Completion, buffered.ResponseFingerprint)

	wantPrevious, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *buffered.ResponseFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	second := decodeResponsesFingerprintRequest(t, `{
		"model":"m",
		"tools":[{"type":"function","name":"search","parameters":{"type":"object"}}],
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"start"}]},
			{"type":"function_call","id":"call_1","call_id":"call_1","status":"completed","name":"search","arguments":"{\"q\":\"one\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"result"}
		]}`)
	assertCurrentResponsesToolResult(t, second, wantPrevious)

	checkpointReasoning, ok := response.Items()[0].Reasoning()
	if !ok {
		t.Fatal("canonical checkpoint truth lost reasoning item")
	}
	if _, ok := checkpointReasoning.Opaque().Messages(); !ok {
		t.Fatal("canonical checkpoint truth lost Messages opaque thinking")
	}
}

func assertResponsesCompletionFingerprint(t *testing.T, completion *wire.ResponseCompletion, want *historyfingerprint.Response) {
	t.Helper()
	got := completion.Snapshot()
	if got.State != wire.CompletionCompleted || got.ResponseFingerprint == nil || want == nil || *got.ResponseFingerprint != *want {
		t.Fatalf("tool stream completion = %#v, want %#v", got, want)
	}
}

func TestResponsesHistoryGroupsMultipleCallsOutputsAndRelationshipIDs(t *testing.T) {
	base := `{
		"model":"m",
		"tools":[{"type":"function","name":"search","parameters":{"type":"object"}}],
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"start"}]},
			{"type":"function_call","id":"item_1","call_id":"call_1","name":"search","arguments":"{\"q\":\"one\"}"},
			{"type":"function_call","id":"item_2","call_id":"call_2","name":"search","arguments":"{\"q\":\"two\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"one"},
			{"type":"function_call_output","call_id":"call_2","output":"two"},
			{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"done"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}
		]}`
	decoded := decodeResponsesFingerprintRequest(t, base)
	if decoded.RebasedRequest == nil || len(decoded.RebasedRequest.Request.Items()) != 2 {
		t.Fatalf("grouped rebased request = %#v, want request declarations and current user contribution", decoded.RebasedRequest)
	}

	changedID := strings.Replace(base, `"call_id":"call_2"`, `"call_id":"call_changed"`, 1)
	changed := decodeResponsesFingerprintRequest(t, changedID)
	if changed.RebasedRequest == nil || changed.RebasedRequest.Previous == decoded.RebasedRequest.Previous {
		t.Fatal("changing a response relationship id did not change completed history")
	}

	reordered := strings.Replace(base,
		`{"type":"function_call","id":"item_1","call_id":"call_1","name":"search","arguments":"{\"q\":\"one\"}"},
			{"type":"function_call","id":"item_2","call_id":"call_2","name":"search","arguments":"{\"q\":\"two\"}"}`,
		`{"type":"function_call","id":"item_2","call_id":"call_2","name":"search","arguments":"{\"q\":\"two\"}"},
			{"type":"function_call","id":"item_1","call_id":"call_1","name":"search","arguments":"{\"q\":\"one\"}"}`, 1)
	reorderedResult := decodeResponsesFingerprintRequest(t, reordered)
	if reorderedResult.RebasedRequest == nil || reorderedResult.RebasedRequest.Previous == decoded.RebasedRequest.Previous {
		t.Fatal("changing response item order did not change completed history")
	}
}

func TestResponsesHistoryResumesAtCurrentFunctionCallOutput(t *testing.T) {
	first := decodeResponsesFingerprintRequest(t, `{"model":"m","input":"start"}`)
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
	second := decodeResponsesFingerprintRequest(t, `{
		"model":"m",
		"tools":[{"type":"function","name":"search","parameters":{"type":"object"}}],
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"start"}]},
			{"type":"function_call","id":"call_1","call_id":"call_1","status":"completed","name":"search","arguments":"{\"q\":\"one\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"result"}
		]}`)
	assertCurrentResponsesToolResult(t, second, wantPrevious)
	direct := decodeResponsesFingerprintRequest(t, `{
		"model":"m","previous_response_id":"swobu_1",
		"input":[{"type":"function_call_output","call_id":"call_1","output":"result"}]
	}`)
	if second.RequestFingerprint != direct.RequestFingerprint {
		t.Fatal("rebased function-call-output fingerprint differs from the same direct contribution")
	}
}

func TestResponsesBufferedAndStreamingCustomHistoryConverge(t *testing.T) {
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
	if !strings.Contains(string(body), `"type":"custom_tool_call"`) || !strings.Contains(string(body), `"input":"echo exact"`) {
		t.Fatalf("custom Responses stream = %s", body)
	}
	got := streamed.Completion.Snapshot()
	if got.State != wire.CompletionCompleted || got.ResponseFingerprint == nil || buffered.ResponseFingerprint == nil || *got.ResponseFingerprint != *buffered.ResponseFingerprint {
		t.Fatalf("custom stream completion = %#v, want %#v", got, buffered.ResponseFingerprint)
	}
}

func TestResponsesRebasedCurrentInputIgnoresUnknownKnownItemFields(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"type":"message","role":"user","content":"one"},{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer"}]},{"type":"message","role":"user","content":"two","future":null,"large":9007199254740993}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Request.RebasedRequest == nil {
		t.Fatal("expected implicit history rebase")
	}
	items := decoded.Request.RebasedRequest.Request.Items()
	if len(items) != 1 || items[0].Kind() != canonical.ItemKindMessage {
		t.Fatalf("rebased canonical input = %#v", items)
	}
}

func TestResponsesRequestFingerprintIgnoresUnknownKnownItemFields(t *testing.T) {
	decode := func(value int) historyfingerprint.Request {
		t.Helper()
		raw := []byte(fmt.Sprintf(`{"model":"m","input":[{"type":"message","role":"user","content":"hello","future":%d}]}`, value))
		decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
		if err != nil {
			t.Fatal(err)
		}
		return decoded.Request.RequestFingerprint
	}
	if decode(1) != decode(2) {
		t.Fatal("unknown known-item field changed request identity")
	}
}

func TestFunctionCallArgumentRepresentationsFingerprintEqually(t *testing.T) {
	object, err := fingerprintResponsesResponseValue([]responsesHistoryItemDTO{{
		Type: "function_call", CallID: "call_1", Name: "search", Arguments: json.RawMessage(`{"q":"one"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	stringified, err := fingerprintResponsesResponseValue([]responsesHistoryItemDTO{{
		Type: "function_call", CallID: "call_1", Name: "search", Arguments: json.RawMessage(`"{\"q\":\"one\"}"`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if object != stringified {
		t.Fatal("object and stringified function arguments produced different history fingerprints")
	}
}

func assertCurrentResponsesToolResult(t *testing.T, decoded wire.ClientRequestResult, previous historyfingerprint.History) {
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

func TestResponsesHistoryPreservesMultimodalContent(t *testing.T) {
	base := `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"see"},{"type":"input_image","image_url":"https://example.test/one.png"}]},{"type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"seen"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}]}`
	decoded := decodeResponsesFingerprintRequest(t, base)
	changed := decodeResponsesFingerprintRequest(t, strings.Replace(base, "one.png", "two.png", 1))
	if decoded.RebasedRequest == nil || changed.RebasedRequest == nil || decoded.RebasedRequest.Previous == changed.RebasedRequest.Previous {
		t.Fatal("changing historical image content did not change completed history")
	}
}

func TestCodexCompletedWebSearchReplayResolvesCheckpoint(t *testing.T) {
	first := decodeResponsesFingerprintRequest(t, `{"model":"m","tools":[{"type":"web_search"}],"input":"find news"}`)
	callID, _ := canonical.NewToolCallID("search_1")
	input, _ := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"find news"}})
	call, _ := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	resultValue, _ := canonical.NewWebSearchResult(nil)
	result, _ := canonical.NewWebSearchResultItem(callID, resultValue)
	response := canonicaltest.Response(t, "swobu_search", "m", []canonical.CanonicalItem{call, result}, canonical.Incomplete("continuation"))
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	wantPrevious, err := historyfingerprint.Advance(nil, first.RequestFingerprint, *encoded.ResponseFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	replayed := decodeResponsesFingerprintRequest(t, `{"model":"m","tools":[{"type":"web_search"}],"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"find news"}]},{"type":"web_search_call","status":"completed"}]}`)
	if replayed.RebasedRequest == nil || replayed.RebasedRequest.Previous != wantPrevious {
		t.Fatalf("web-search replay = %#v, want predecessor %#v", replayed.RebasedRequest, wantPrevious)
	}
	items := replayed.RebasedRequest.Request.Items()
	if len(items) != 1 || items[0].Kind() != canonical.ItemKindToolDeclarations {
		t.Fatalf("web-search current input = %#v, want only active tool declarations", items)
	}
}

func decodeResponsesFingerprintRequest(t *testing.T, raw string) wire.ClientRequestResult {
	t.Helper()
	result, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(raw), carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	return result.Request
}
