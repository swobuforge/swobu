package messages

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestMessagesAllErasedToolResultDoesNotClosePendingCall(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"lookup","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[
			{"type":"future_result"}
		]}]}
	]}`)
	if _, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Messages, "application/json", nil, raw, carrier.Meta{}),
	); err == nil {
		t.Fatal("all-erased Messages tool result closed its pending call")
	}
}

func TestMessagesBufferedRejectsMissingBlockType(t *testing.T) {
	_, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, []byte(`{
		"id":"msg_1","model":"m","stop_reason":"end_turn","content":[
			{"text":"hidden"},
			{"type":"text","text":"visible"}
		]
	}`), "ex", nil)
	if err == nil || !strings.Contains(err.Error(), "backend error") {
		t.Fatalf("decode error = %v, want backend-origin missing discriminator", err)
	}
}

func TestMessagesStreamRejectsMissingBlockType(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"text\":\"hidden\"}}\n\n"
	assertMessagesStreamBackendError(t, raw)
}

func TestMessagesStreamRejectsLifecycleBeforeBlockStart(t *testing.T) {
	tests := map[string]string{
		"delta": "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"orphan\"}}\n\n",
		"stop":  "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
	}
	for name, frame := range tests {
		t.Run(name, func(t *testing.T) {
			raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" + frame
			assertMessagesStreamBackendError(t, raw)
		})
	}
}

func TestMessagesStreamRejectsIncompatibleKnownDeltaBeforeEmission(t *testing.T) {
	tests := []struct {
		name      string
		request   canonical.CanonicalRequest
		block     string
		delta     string
		forbidden canonical.EventKind
	}{
		{
			name:      "text with input json",
			block:     `{"type":"text"}`,
			delta:     `{"type":"input_json_delta","partial_json":"{}"}`,
			forbidden: canonical.EventArgsDelta,
		},
		{
			name:      "tool use with text",
			request:   messagesStreamFunctionRequest(t),
			block:     `{"type":"tool_use","id":"call_1","name":"search","input":{}}`,
			delta:     `{"type":"text_delta","text":"hidden"}`,
			forbidden: canonical.EventTextDelta,
		},
		{
			name:      "thinking with input json",
			block:     `{"type":"thinking","thinking":"","signature":""}`,
			delta:     `{"type":"input_json_delta","partial_json":"{}"}`,
			forbidden: canonical.EventArgsDelta,
		},
		{
			name:      "text with signature",
			block:     `{"type":"text"}`,
			delta:     `{"type":"signature_delta","signature":"sig"}`,
			forbidden: canonical.EventTextDelta,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":" + test.block + "}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":" + test.delta + "}\n\n"
			stream := decodeResponseStream(test.request, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
			for {
				event, err := stream.Next(context.Background())
				if event.Kind == test.forbidden {
					t.Fatalf("emitted incompatible %s before provider error", event.Kind)
				}
				if err == nil {
					continue
				}
				if err == io.EOF || !strings.Contains(err.Error(), "backend error") {
					t.Fatalf("stream error = %v, want backend-origin incompatible delta", err)
				}
				return
			}
		})
	}
}

func TestMessagesBlockDeltaAdmissionMatrix(t *testing.T) {
	knownDeltaTypes := []string{
		"text_delta",
		"input_json_delta",
		"thinking_delta",
		"signature_delta",
		"citations_delta",
		"citation_delta",
	}
	tests := []struct {
		name  string
		block streamContentBlock
		want  map[string]bool
	}{
		{
			name:  "text",
			block: streamContentBlock{ItemKind: canonical.ItemKindMessage},
			want: map[string]bool{
				"text_delta": true, "citations_delta": true, "citation_delta": true,
			},
		},
		{
			name:  "function tool call",
			block: streamContentBlock{ItemKind: canonical.ItemKindToolCall, Tool: canonical.ToolKey{}},
			want:  map[string]bool{"input_json_delta": true},
		},
		{
			name:  "web search call",
			block: streamContentBlock{ItemKind: canonical.ItemKindToolCall, Tool: canonical.WebSearchToolKey()},
			want:  map[string]bool{"input_json_delta": true},
		},
		{
			name:  "thinking",
			block: streamContentBlock{ItemKind: canonical.ItemKindReasoning, reasoningType: "thinking"},
			want:  map[string]bool{"thinking_delta": true, "signature_delta": true},
		},
		{
			name:  "redacted thinking",
			block: streamContentBlock{ItemKind: canonical.ItemKindReasoning, reasoningType: "redacted_thinking"},
			want:  map[string]bool{},
		},
		{
			name:  "web search result",
			block: streamContentBlock{ItemKind: canonical.ItemKindToolResult},
			want:  map[string]bool{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, deltaType := range knownDeltaTypes {
				if got := messagesBlockAdmitsDelta(&test.block, deltaType); got != test.want[deltaType] {
					t.Errorf("delta %q admitted = %v, want %v", deltaType, got, test.want[deltaType])
				}
			}
		})
	}
}

func TestMessagesStreamRejectsMissingDeltaDiscriminatorWithoutDrop(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"text\":\"hidden\"}}\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	assertMessagesReaderBackendErrorWithoutDrop(t, stream)
}

func TestMessagesStreamRejectsMissingTopLevelDataDiscriminatorWithoutDrop(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: future_delta\ndata: {\"index\":0,\"value\":\"hidden\"}\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	assertMessagesReaderBackendErrorWithoutDrop(t, stream)
}

func assertMessagesReaderBackendErrorWithoutDrop(t *testing.T, stream *messagesEventReader) {
	t.Helper()
	for {
		_, err := stream.Next(context.Background())
		if err == nil {
			continue
		}
		if err == io.EOF || !strings.Contains(err.Error(), "backend error") {
			t.Fatalf("stream error = %v, want backend-origin missing discriminator", err)
		}
		for _, decision := range stream.Changes() {
			if decision.Kind == compat.Omission {
				t.Fatalf("missing discriminator recorded as additive erasure: %#v", stream.Changes())
			}
		}
		return
	}
}

func messagesStreamFunctionRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "search")
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	declaration, _ := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	tools, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{tools}})
}

func assertMessagesStreamBackendError(t *testing.T, raw string) {
	t.Helper()
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	for {
		_, err := stream.Next(context.Background())
		if err == nil {
			continue
		}
		if err == io.EOF {
			t.Fatal("malformed Messages lifecycle reached EOF")
		}
		if !strings.Contains(err.Error(), "backend error") {
			t.Fatalf("stream error = %v, want backend-origin lifecycle error", err)
		}
		return
	}
}

func TestMessagesStreamRejectsInvalidResponseLifecycle(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "content block before start",
			raw:  "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n",
		},
		{
			name: "message delta before start",
			raw:  "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n",
		},
		{
			name: "duplicate start",
			raw: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
				"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n",
		},
		{
			name: "stop before start",
			raw:  "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		},
		{
			name: "frame after stop",
			raw: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
				"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMessagesStreamBackendError(t, test.raw)
		})
	}
}

func TestMessagesStreamEOFBeforeMessageStopIsTerminalError(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	var sawError, sawErrorEnd bool
	for {
		event, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if event.Kind == canonical.EventError {
			sawError = true
		}
		if event.Kind == canonical.EventEnvelopeEnd {
			payload := event.Payload.(canonical.EnvelopeEndPayload)
			sawErrorEnd = payload.Status == canonical.EnvelopeStatusError
		}
	}
	if !sawError || !sawErrorEnd {
		t.Fatalf("premature EOF terminal events: error=%v error-end=%v", sawError, sawErrorEnd)
	}
}

func TestMessagesStreamEOFBeforeMessageStartIsBackendError(t *testing.T) {
	assertMessagesStreamBackendError(t, "")
}

func TestMessagesStreamUnknownBlockCannotSatisfyToolUseStop(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"future_tool_use\",\"id\":\"call_1\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"future_delta\",\"text\":\"one\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"future_delta\",\"text\":\"two\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	for {
		_, err := stream.Next(context.Background())
		if err != nil {
			if err == io.EOF {
				t.Fatal("unknown-only tool block reached successful stream EOF")
			}
			if len(stream.Changes()) != 1 || stream.Changes()[0].Kind != compat.Omission {
				t.Fatalf("compatibility changes = %#v", stream.Changes())
			}
			return
		}
	}
}

func TestMessagesStreamRejectsDuplicateOrReusedBlockIndex(t *testing.T) {
	tests := []struct {
		name   string
		frames string
	}{
		{
			name: "unknown to known",
			frames: "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"future_block\"}}\n\n" +
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n",
		},
		{
			name: "known to unknown",
			frames: "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n" +
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"future_block\"}}\n\n",
		},
		{
			name: "second known start",
			frames: "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n" +
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n",
		},
		{
			name: "resolved index reused",
			frames: "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"first\"}}\n\n" +
				"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
				"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" + test.frames
			stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
			for {
				_, err := stream.Next(context.Background())
				if err == nil {
					continue
				}
				if err == io.EOF {
					t.Fatal("duplicate or reused block index reached EOF without provider contradiction")
				}
				if !strings.Contains(err.Error(), "backend error") {
					t.Fatalf("stream error = %v, want backend-origin block contradiction", err)
				}
				return
			}
		})
	}
}

func TestMessagesStreamRejectsOutOfOrderBlockStarts(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\"}}\n\n"
	assertMessagesStreamBackendError(t, raw)
}

func TestMessagesStreamUnknownBlockAdvancesProviderIndexWithoutCanonicalOrdinal(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"future_block\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"kept\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	var completed *canonical.ItemEvent
	for {
		event, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if event.Kind == canonical.EventItemCompleted {
			payload := event.Payload.(canonical.ItemEvent)
			completed = &payload
		}
	}
	if completed == nil || completed.Position.Item != 0 {
		t.Fatalf("completed item = %#v, want compact canonical ordinal zero", completed)
	}
}

func TestMessagesStreamToolUseFinishRequiresCompletedToolCall(t *testing.T) {
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "search")
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	declaration, _ := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	tools, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{tools}})
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_1\",\"name\":\"search\",\"input\":{}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	stream := decodeResponseStream(request, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	for {
		_, err := stream.Next(context.Background())
		if err == nil {
			continue
		}
		if err == io.EOF {
			t.Fatal("started but incomplete tool call satisfied tool_use finish")
		}
		return
	}
}

func TestMessagesStreamRecordsUnknownDeltaKindOncePerKnownBlock(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"future_delta\",\"text\":\"one\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"future_delta\",\"text\":\"two\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	for {
		_, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	drops := 0
	for _, decision := range stream.Changes() {
		item, ok := decision.Occurrence.ResponseItem()
		if ok && item == 0 {
			drops++
		}
	}
	if drops != 1 {
		t.Fatalf("compatibility changes = %#v, want one unknown-delta decision per known block", stream.Changes())
	}
}

func TestMessagesStreamIgnoresUnknownTopLevelWireEvents(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: future_delta\ndata: {\"type\":\"future_delta\",\"index\":3,\"value\":\"one\"}\n\n" +
		"event: future_delta\ndata: {\"type\":\"future_delta\",\"index\":3,\"value\":\"two\"}\n\n" +
		"event: future_delta\ndata: {\"type\":\"future_delta\",\"index\":4,\"value\":\"other block\"}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	for {
		_, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(stream.Changes()) != 0 {
		t.Fatalf("wire-only events created semantic changes: %#v", stream.Changes())
	}
}

func TestMessagesStreamIgnoresUnknownTopLevelEventIDs(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: future_delta\ndata: {\"type\":\"future_delta\",\"id\":\"event_1\",\"value\":\"one\"}\n\n" +
		"event: future_delta\ndata: {\"type\":\"future_delta\",\"id\":\"event_2\",\"value\":\"two\"}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	for {
		_, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(stream.Changes()) != 0 {
		t.Fatalf("wire-only events created semantic changes: %#v", stream.Changes())
	}
}
