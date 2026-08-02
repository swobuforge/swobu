package chatcompletions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestChatResponseUnknownOnlyCallCannotSatisfyToolCallsFinish(t *testing.T) {
	var changes []compat.Change
	raw := []byte(`{"id":"chat_1","model":"m","choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"type":"future_call","id":""}]},"finish_reason":"tool_calls"}]}`)
	if _, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex", &changes); err == nil {
		t.Fatal("unknown-only tool call satisfied tool_calls finish reason")
	}
	if len(changes) != 1 || changes[0].Kind != compat.Omission {
		t.Fatalf("compatibility changes = %#v, want erased-call evidence", changes)
	}
}

func TestChatStreamRejectsContradictoryTypeAfterToolAdmission(t *testing.T) {
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "search")
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	declaration, _ := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	tools, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{tools}})
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{\"}}]}}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"future_call\"}]}}]}\n\n"
	stream := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	for {
		_, err := stream.Next(context.Background())
		if err == nil {
			continue
		}
		if err == io.EOF {
			t.Fatal("contradictory late tool type reached stream EOF")
		}
		if len(stream.Changes()) != 0 {
			t.Fatalf("late contradiction was recorded as erasure: %#v", stream.Changes())
		}
		return
	}
}

func TestChatStreamRejectsReclassifiedUnknownToolOccurrence(t *testing.T) {
	tests := []struct {
		name     string
		lateType string
	}{
		{name: "unknown to known", lateType: "function"},
		{name: "unknown to different unknown", lateType: "other_future_call"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "search")
			schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
			declaration, _ := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
			set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
			tools, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{tools}})
			raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"future_call\"}]}}]}\n\n" +
				"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"visible\",\"tool_calls\":[{\"index\":0,\"type\":\"" + test.lateType + "\",\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"stop\"}]}\n\n"
			stream := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
			for {
				_, err := stream.Next(context.Background())
				if err == nil {
					continue
				}
				if err == io.EOF {
					t.Fatal("reclassified unknown tool occurrence reached successful EOF")
				}
				if !strings.Contains(err.Error(), "backend error") {
					t.Fatalf("stream error = %v, want backend-origin contradiction", err)
				}
				return
			}
		})
	}
}

func TestChatStreamKeepsExactUnknownToolOccurrenceErased(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"future_call\"}]}}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"visible\",\"tool_calls\":[{\"index\":0,\"type\":\"future_call\",\"id\":\"ignored\"}]},\"finish_reason\":\"stop\"}]}\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
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
		if decision.Capability == canonical.ResponseItemsKind && decision.Kind == compat.Omission {
			drops++
		}
	}
	if drops != 1 {
		t.Fatalf("unknown tool evidence = %#v, want one retained erasure", stream.Changes())
	}
}

func TestChatStreamDoesNotEraseKnownCustomToolCall(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"visible\",\"tool_calls\":[{\"index\":0,\"type\":\"custom\",\"id\":\"call_1\",\"custom\":{\"name\":\"shell\",\"input\":\"echo hi\"}}]},\"finish_reason\":\"stop\"}]}\n\n"
	request := chatStreamCustomRequest(t)
	stream := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	found := false
	for {
		event, err := stream.Next(context.Background())
		if err == nil {
			if event.Kind == canonical.EventItemCompleted {
				completed := event.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item
				if call, ok := completed.ToolCall(); ok {
					input, text := call.Input().Text()
					found = text && call.Tool().Kind() == canonical.ToolKindCustom && input == "echo hi"
				}
			}
			continue
		}
		if err == io.EOF {
			break
		}
		t.Fatal(err)
	}
	if !found {
		t.Fatal("known streamed custom tool call did not preserve raw input")
	}
	for _, decision := range stream.Changes() {
		if decision.Capability == canonical.ResponseItemsKind {
			t.Fatalf("known custom tool call recorded as additive erasure: %#v", stream.Changes())
		}
	}
}

func TestChatStreamingCustomDeclarationReachesExactFragmentedCall(t *testing.T) {
	request := chatStreamCustomRequest(t)
	document, err := LowerProviderRequestDocument(request, testAttemptToolNames(request), delivery.StreamingDelivery(delivery.FramingSSE), nil, "ex")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Tools) != 1 || document.Tools[0].Type != canonical.ToolTypeCustom ||
		document.Tools[0].Custom == nil || document.Tools[0].Custom.Name != "shell" {
		t.Fatalf("encoded tools = %#v, want custom shell declaration", document.Tools)
	}

	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"custom\",\"id\":\"call_1\",\"custom\":{\"name\":\"shell\",\"input\":\"echo \"}}]}}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"custom\":{\"input\":\"hi\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	completed := drainChatStreamCompletedItems(t, request, raw)
	if len(completed) != 1 {
		t.Fatalf("completed items = %#v, want one custom call", completed)
	}
	call, ok := completed[0].item.ToolCall()
	if !ok {
		t.Fatalf("completed item = %#v, want custom call", completed[0])
	}
	input, text := call.Input().Text()
	if call.Tool().Kind() != canonical.ToolKindCustom || !text || input != "echo hi" {
		t.Fatalf("completed call = %#v, input = %q, text = %v", completed[0], input, text)
	}
}

func TestChatStreamInfersCustomKindFromExclusiveBody(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"custom\":{\"name\":\"shell\",\"input\":\"echo hi\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	completed := drainChatStreamCompletedItems(t, chatStreamCustomRequest(t), raw)
	if len(completed) != 1 {
		t.Fatalf("completed items = %#v, want one custom call", completed)
	}
	call, ok := completed[0].item.ToolCall()
	if !ok {
		t.Fatalf("completed item = %#v, want custom call", completed[0])
	}
	input, text := call.Input().Text()
	if call.Tool().Kind() != canonical.ToolKindCustom || !text || input != "echo hi" {
		t.Fatalf("completed call = %#v, input = %q, text = %v", completed[0], input, text)
	}
}

func TestChatBufferedRejectsIncompleteOrContradictoryToolCallUnion(t *testing.T) {
	request := chatStreamMixedToolRequest(t)
	tests := []struct {
		name string
		call string
	}{
		{
			name: "missing type and body",
			call: `{"id":"call_1"}`,
		},
		{
			name: "function with custom body",
			call: `{"type":"function","id":"call_1","function":{"name":"search","arguments":"{}"},"custom":{"name":"shell","input":"hidden"}}`,
		},
		{
			name: "custom with function body",
			call: `{"type":"custom","id":"call_1","custom":{"name":"shell","input":"visible"},"function":{"name":"search","arguments":"{}"}}`,
		},
		{
			name: "missing type with both bodies",
			call: `{"id":"call_1","custom":{"name":"shell","input":"visible"},"function":{"name":"search","arguments":"{}"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := `{"id":"chat_1","model":"m","choices":[{"message":{"content":"visible","tool_calls":[` + test.call + `]},"finish_reason":"stop"}]}`
			if _, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), []byte(raw), "ex", nil); err == nil || !strings.Contains(err.Error(), "backend error") {
				t.Fatalf("buffered error = %v, want backend-origin malformed union", err)
			}
		})
	}
}

func TestChatStreamUnresolvedToolCallLaterResolvesToFunction(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"function\",\"function\":{\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"search\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	completed := drainChatStreamCompletedItems(t, chatStreamFunctionRequest(t), raw)
	if len(completed) != 1 {
		t.Fatalf("completed items = %#v, want one function call", completed)
	}
	call, ok := completed[0].item.ToolCall()
	if !ok || call.Tool().Kind() != canonical.ToolKindFunction {
		t.Fatalf("completed item = %#v, want function call", completed[0])
	}
}

func TestChatStreamRejectsConflictingToolIdentity(t *testing.T) {
	tests := []struct {
		name   string
		second string
	}{
		{name: "call id", second: `{"index":0,"id":"call_2","function":{"arguments":"}"}}`},
		{name: "tool name", second: `{"index":0,"function":{"name":"delete","arguments":"}"}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"function\",\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{\"}}]}}]}\n\n" +
				"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[" + test.second + "]},\"finish_reason\":\"tool_calls\"}]}\n\n"
			stream := decodeResponseStream(chatStreamFunctionRequest(t), nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
			assertChatStreamBackendError(t, stream)
		})
	}
}

func TestChatStreamAcceptsRepeatedToolIdentity(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"function\",\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{\"}}]}}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	completed := drainChatStreamCompletedItems(t, chatStreamFunctionRequest(t), raw)
	if len(completed) != 1 {
		t.Fatalf("completed items = %#v, want one repeated-identity call", completed)
	}
}

func TestChatStreamRejectsConflictingResponseIdentity(t *testing.T) {
	tests := []struct {
		name   string
		second string
	}{
		{name: "response id", second: `{"id":"chat_2","model":"m"}`},
		{name: "model", second: `{"id":"chat_1","model":"m2"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[]}\n\n" +
				"data: " + test.second[:len(test.second)-1] + ",\"choices\":[]}\n\n"
			stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
			assertChatStreamBackendError(t, stream)
		})
	}
}

func TestChatStreamAcceptsRepeatedResponseIdentity(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"
	completed := drainChatStreamCompletedItems(t, canonical.CanonicalRequest{}, raw)
	if len(completed) != 1 {
		t.Fatalf("completed items = %#v, want repeated envelope identity accepted", completed)
	}
}

func TestChatStreamCompletesPreviouslyAbsentResponseIdentity(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"choices\":[]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"
	completed := drainChatStreamCompletedItems(t, canonical.CanonicalRequest{}, raw)
	if len(completed) != 1 {
		t.Fatalf("completed items = %#v, want late model completion accepted", completed)
	}
}

func TestChatStreamRejectsTerminallyUnresolvedToolCall(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"visible\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\"}]},\"finish_reason\":\"stop\"}]}\n\n"
	stream := decodeResponseStream(chatStreamFunctionRequest(t), nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	assertChatStreamBackendError(t, stream)
}

func TestChatStreamRejectsBodyBranchChangeWithoutExplicitType(t *testing.T) {
	tests := []struct {
		name   string
		first  string
		second string
	}{
		{
			name:   "function then custom body",
			first:  `{"index":0,"type":"function","id":"call_1","function":{"name":"search","arguments":"{}"}}`,
			second: `{"index":0,"custom":{"input":"hidden"}}`,
		},
		{
			name:   "custom then function body",
			first:  `{"index":0,"type":"custom","id":"call_1","custom":{"name":"shell","input":"visible"}}`,
			second: `{"index":0,"function":{"arguments":"{}"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[" + test.first + "]}}]}\n\n" +
				"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[" + test.second + "]},\"finish_reason\":\"tool_calls\"}]}\n\n"
			stream := decodeResponseStream(chatStreamMixedToolRequest(t), nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
			assertChatStreamBackendError(t, stream)
		})
	}
}

func TestChatStreamRejectsUnobservedToolIndexGap(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":2,\"type\":\"function\",\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	stream := decodeResponseStream(chatStreamFunctionRequest(t), nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	assertChatStreamBackendError(t, stream)
}

func TestChatStreamRejectsNegativeToolIndex(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":-1,\"type\":\"function\",\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	stream := decodeResponseStream(chatStreamFunctionRequest(t), nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	assertChatStreamBackendError(t, stream)
}

func TestChatStreamPreservesCustomInputPartitionsExactly(t *testing.T) {
	tests := []struct {
		name      string
		fragments []string
		want      string
	}{
		{name: "fragmented", fragments: []string{"patch ", "line\n", "  end"}, want: "patch line\n  end"},
		{name: "empty", fragments: nil, want: ""},
		{name: "whitespace exact", fragments: []string{" \t", "\n ", "x  "}, want: " \t\n x  "},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var raw strings.Builder
			fmt.Fprintf(&raw, "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"custom\",\"id\":\"call_1\",\"custom\":{\"name\":\"shell\"}}]}}]}\n\n")
			for _, fragment := range test.fragments {
				encoded, err := json.Marshal(fragment)
				if err != nil {
					t.Fatal(err)
				}
				fmt.Fprintf(&raw, "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"custom\":{\"input\":%s}}]}}]}\n\n", encoded)
			}
			raw.WriteString("data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
			completed := drainChatStreamCompletedItems(t, chatStreamCustomRequest(t), raw.String())
			if len(completed) != 1 {
				t.Fatalf("completed items = %#v, want one custom call", completed)
			}
			call, ok := completed[0].item.ToolCall()
			if !ok {
				t.Fatalf("completed item = %#v, want custom call", completed[0])
			}
			input, text := call.Input().Text()
			if !text || input != test.want {
				t.Fatalf("custom input = %q, text=%v, want %q", input, text, test.want)
			}
		})
	}
}

func TestChatStreamProjectsTextBeforeCustomCallRegardlessOfArrival(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"custom\",\"id\":\"call_1\",\"custom\":{\"name\":\"shell\",\"input\":\"echo hi\"}}]}}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"visible\"},\"finish_reason\":\"tool_calls\"}]}\n\n"
	completed := drainChatStreamCompletedItems(t, chatStreamCustomRequest(t), raw)
	if len(completed) != 2 || completed[0].item.Kind() != canonical.ItemKindMessage || completed[0].ordinal != 0 {
		t.Fatalf("completed items = %#v, want message first", completed)
	}
	call, ok := completed[1].item.ToolCall()
	if !ok || call.Tool().Kind() != canonical.ToolKindCustom || completed[1].ordinal != 1 {
		t.Fatalf("completed items = %#v, want custom call second", completed)
	}
}

func TestChatStreamOrdersFunctionAndCustomCallsTogether(t *testing.T) {
	request := chatStreamMixedToolRequest(t)
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"type\":\"custom\",\"id\":\"call_custom\",\"custom\":{\"name\":\"shell\",\"input\":\"echo hi\"}}]}}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"function\",\"id\":\"call_function\",\"function\":{\"name\":\"search\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	completed := drainChatStreamCompletedItems(t, request, raw)
	if len(completed) != 2 {
		t.Fatalf("completed items = %#v, want function and custom calls", completed)
	}
	first, firstOK := completed[0].item.ToolCall()
	second, secondOK := completed[1].item.ToolCall()
	if !firstOK || !secondOK ||
		first.Tool().Kind() != canonical.ToolKindFunction ||
		second.Tool().Kind() != canonical.ToolKindCustom ||
		completed[0].ordinal != 0 || completed[1].ordinal != 1 {
		t.Fatalf("completed items = %#v, want provider-index function then custom", completed)
	}
}

func TestChatBufferedAndStreamedCustomCallsAreSemanticallyEquivalent(t *testing.T) {
	request := chatStreamCustomRequest(t)
	bufferedRaw := []byte(`{
		"id":"chat_1",
		"model":"m",
		"choices":[{
			"message":{
				"role":"assistant",
				"content":"visible",
				"tool_calls":[{
					"type":"custom",
					"id":"call_1",
					"custom":{"name":"shell","input":" \tpatch\n "}
				}]
			},
			"finish_reason":"tool_calls"
		}]
	}`)
	buffered, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), bufferedRaw, "ex-buffered", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamedRaw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"custom\",\"id\":\"call_1\",\"custom\":{\"name\":\"shell\",\"input\":\" \\t\"}}]}}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"visible\",\"tool_calls\":[{\"index\":0,\"custom\":{\"input\":\"patch\\n \"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	streamed := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(streamedRaw))}, "ex-streamed", nil)
	bufferedItems := drainChatCompletedItems(t, buffered)
	streamedItems := drainChatCompletedItems(t, streamed)
	assertEquivalentChatResponseItems(t, bufferedItems, streamedItems)
}

func TestChatStreamRejectsEveryToolKindReclassification(t *testing.T) {
	tests := []struct {
		name       string
		firstType  string
		secondType string
		firstBody  string
	}{
		{name: "function to custom", firstType: "function", secondType: "custom", firstBody: `,"id":"call_1","function":{"name":"search","arguments":"{}"}`},
		{name: "function to unknown", firstType: "function", secondType: "future_call", firstBody: `,"id":"call_1","function":{"name":"search","arguments":"{}"}`},
		{name: "custom to function", firstType: "custom", secondType: "function", firstBody: `,"id":"call_1","custom":{"name":"shell","input":""}`},
		{name: "custom to unknown", firstType: "custom", secondType: "future_call", firstBody: `,"id":"call_1","custom":{"name":"shell","input":""}`},
		{name: "unknown to function", firstType: "future_call", secondType: "function"},
		{name: "unknown to custom", firstType: "future_call", secondType: "custom"},
		{name: "unknown to different unknown", firstType: "future_call", secondType: "other_future_call"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"" + test.firstType + "\"" + test.firstBody + "}]}}]}\n\n" +
				"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"" + test.secondType + "\"}]}}]}\n\n"
			stream := decodeResponseStream(chatStreamMixedToolRequest(t), nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
			assertChatStreamBackendError(t, stream)
		})
	}
}

func TestChatStreamCompactsCustomCallAfterErasedUnknownOccurrence(t *testing.T) {
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"future_call\"},{\"index\":1,\"type\":\"custom\",\"id\":\"call_1\",\"custom\":{\"name\":\"shell\",\"input\":\"exact\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	completed := drainChatStreamCompletedItems(t, chatStreamCustomRequest(t), raw)
	if len(completed) != 1 || completed[0].ordinal != 0 {
		t.Fatalf("completed items = %#v, want compact custom ordinal 0", completed)
	}
	call, ok := completed[0].item.ToolCall()
	if !ok || call.Tool().Kind() != canonical.ToolKindCustom {
		t.Fatalf("completed item = %#v, want custom call", completed[0])
	}
}

func TestChatMalformedKnownToolCallsAreBackendErrors(t *testing.T) {
	request := chatStreamMixedToolRequest(t)
	bufferedCases := []string{
		`{"id":"chat_1","model":"m","choices":[{"message":{"tool_calls":[{"type":"custom","id":"call_1","custom":{"input":"x"}}]},"finish_reason":"tool_calls"}]}`,
		`{"id":"chat_1","model":"m","choices":[{"message":{"tool_calls":[{"type":"function","id":"call_1","function":{"name":"search","arguments":"{"}}]},"finish_reason":"tool_calls"}]}`,
	}
	for _, raw := range bufferedCases {
		if _, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), []byte(raw), "ex", nil); err == nil || !strings.Contains(err.Error(), "backend error") {
			t.Fatalf("buffered error = %v, want backend-origin malformed known call", err)
		}
	}
	streamCases := []string{
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"custom\",\"id\":\"call_1\",\"custom\":{\"input\":\"x\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n",
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"function\",\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n",
	}
	for _, raw := range streamCases {
		stream := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
		assertChatStreamBackendError(t, stream)
	}
}

func TestChatStreamCompactsOrdinalsAfterErasedToolCalls(t *testing.T) {
	tests := []struct {
		name         string
		firstDelta   string
		erasedCalls  string
		wantOrdinals []uint32
	}{
		{
			name:         "erased then tool",
			erasedCalls:  `{"index":0,"type":"future_call"}`,
			wantOrdinals: []uint32{0},
		},
		{
			name:         "text erased then tool",
			firstDelta:   `"content":"visible",`,
			erasedCalls:  `{"index":0,"type":"future_call"}`,
			wantOrdinals: []uint32{0, 1},
		},
		{
			name:         "multiple erased then tool",
			erasedCalls:  `{"index":0,"type":"future_call"},{"index":1,"type":"other_future_call"}`,
			wantOrdinals: []uint32{0},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := chatStreamFunctionRequest(t)
			survivingIndex := strings.Count(test.erasedCalls, `"index"`)
			raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{" + test.firstDelta + "\"tool_calls\":[" + test.erasedCalls + "]}}]}\n\n" +
				fmt.Sprintf("data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":%d,\"type\":\"function\",\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", survivingIndex)
			stream := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
			var ordinals []uint32
			for {
				event, err := stream.Next(context.Background())
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if event.Kind != canonical.EventItemStart {
					continue
				}
				itemEvent, ok := event.Payload.(canonical.ItemEvent)
				if !ok {
					t.Fatalf("item start payload = %#v", event.Payload)
				}
				ordinals = append(ordinals, itemEvent.Position.Item)
			}
			if fmt.Sprint(ordinals) != fmt.Sprint(test.wantOrdinals) {
				t.Fatalf("item ordinals = %v, want %v", ordinals, test.wantOrdinals)
			}
		})
	}
}

func TestChatStreamOrdersToolCallsByProviderIndexNotFirstArrival(t *testing.T) {
	request := chatStreamFunctionRequests(t, "a", "b")
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"type\":\"function\",\"id\":\"call_b\",\"function\":{\"name\":\"b\",\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"function\",\"id\":\"call_a\",\"function\":{\"name\":\"a\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	completed := drainChatStreamCompletedItems(t, request, raw)
	assertChatCompletedToolOrder(t, completed, []string{"a", "b"}, []uint32{0, 1})
}

func TestChatStreamToolFragmentBeforeTextStillProjectsMessageThenCall(t *testing.T) {
	request := chatStreamFunctionRequests(t, "search")
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"function\",\"id\":\"call_1\",\"function\":{\"name\":\"search\",\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"visible\"},\"finish_reason\":\"tool_calls\"}]}\n\n"
	completed := drainChatStreamCompletedItems(t, request, raw)
	if len(completed) != 2 {
		t.Fatalf("completed items = %#v, want message then tool call", completed)
	}
	if _, ok := completed[0].item.Message(); !ok || completed[0].ordinal != 0 {
		t.Fatalf("first completed item = %#v, want message at ordinal 0", completed[0])
	}
	call, ok := completed[1].item.ToolCall()
	if !ok || call.Tool().Name() != "search" || completed[1].ordinal != 1 {
		t.Fatalf("second completed item = %#v, want search call at ordinal 1", completed[1])
	}
}

func TestChatStreamCompactsUnknownAndKnownCallsInProviderIndexOrder(t *testing.T) {
	request := chatStreamFunctionRequests(t, "a", "b")
	raw := "data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"future_call\"},{\"index\":2,\"type\":\"function\",\"id\":\"call_b\",\"function\":{\"name\":\"b\",\"arguments\":\"{}\"}}]}}]}\n\n" +
		"data: {\"id\":\"chat_1\",\"model\":\"m\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"type\":\"function\",\"id\":\"call_a\",\"function\":{\"name\":\"a\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"
	stream := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	var completed []chatCompletedItem
	for {
		event, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if event.Kind != canonical.EventItemCompleted {
			continue
		}
		itemEvent := event.Payload.(canonical.ItemEvent)
		completed = append(completed, chatCompletedItem{
			ordinal: itemEvent.Position.Item,
			item:    itemEvent.Payload.(canonical.ItemCompletedPayload).Item,
		})
	}
	assertChatCompletedToolOrder(t, completed, []string{"a", "b"}, []uint32{0, 1})
	erasedCalls := 0
	for _, decision := range stream.Changes() {
		if decision.Capability == canonical.ResponseItemsKind && decision.Kind == compat.Omission {
			erasedCalls++
		}
	}
	if erasedCalls != 1 {
		t.Fatalf("compatibility changes = %#v, want one erased unknown call", stream.Changes())
	}
}

type chatCompletedItem struct {
	ordinal uint32
	item    canonical.CanonicalItem
}

func drainChatCompletedItems(t *testing.T, stream canonical.ResponseStream) []chatCompletedItem {
	t.Helper()
	var completed []chatCompletedItem
	for {
		event, err := stream.Next(context.Background())
		if err == io.EOF {
			return completed
		}
		if err != nil {
			t.Fatal(err)
		}
		if event.Kind != canonical.EventItemCompleted {
			continue
		}
		itemEvent := event.Payload.(canonical.ItemEvent)
		completed = append(completed, chatCompletedItem{
			ordinal: itemEvent.Position.Item,
			item:    itemEvent.Payload.(canonical.ItemCompletedPayload).Item,
		})
	}
}

func drainChatStreamCompletedItems(t *testing.T, request canonical.CanonicalRequest, raw string) []chatCompletedItem {
	t.Helper()
	stream := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	return drainChatCompletedItems(t, stream)
}

func assertChatCompletedToolOrder(t *testing.T, completed []chatCompletedItem, names []string, ordinals []uint32) {
	t.Helper()
	if len(completed) != len(names) {
		t.Fatalf("completed items = %#v, want %d tool calls", completed, len(names))
	}
	for i, completedItem := range completed {
		call, ok := completedItem.item.ToolCall()
		if !ok || call.Tool().Name() != names[i] || completedItem.ordinal != ordinals[i] {
			t.Fatalf("completed item %d = %#v, want tool %q at ordinal %d", i, completedItem, names[i], ordinals[i])
		}
	}
}

func assertEquivalentChatResponseItems(t *testing.T, buffered, streamed []chatCompletedItem) {
	t.Helper()
	if len(buffered) != len(streamed) {
		t.Fatalf("buffered items = %#v, streamed items = %#v", buffered, streamed)
	}
	for index := range buffered {
		if buffered[index].ordinal != streamed[index].ordinal || buffered[index].item.Kind() != streamed[index].item.Kind() {
			t.Fatalf("item %d buffered = %#v, streamed = %#v", index, buffered[index], streamed[index])
		}
		if bufferedMessage, ok := buffered[index].item.Message(); ok {
			streamedMessage, streamedOK := streamed[index].item.Message()
			if !streamedOK || !equivalentChatMessageText(bufferedMessage, streamedMessage) {
				t.Fatalf("message %d buffered = %#v, streamed = %#v", index, bufferedMessage, streamedMessage)
			}
			continue
		}
		bufferedCall, bufferedOK := buffered[index].item.ToolCall()
		streamedCall, streamedOK := streamed[index].item.ToolCall()
		if !bufferedOK || !streamedOK ||
			bufferedCall.CallID() != streamedCall.CallID() ||
			bufferedCall.Tool() != streamedCall.Tool() {
			t.Fatalf("call %d buffered = %#v, streamed = %#v", index, bufferedCall, streamedCall)
		}
		bufferedInput, bufferedText := bufferedCall.Input().Text()
		streamedInput, streamedText := streamedCall.Input().Text()
		if !bufferedText || !streamedText || bufferedInput != streamedInput {
			t.Fatalf("call input %d buffered = %q, streamed = %q", index, bufferedInput, streamedInput)
		}
	}
}

func equivalentChatMessageText(left, right canonical.MessageItem) bool {
	leftContent := left.Content()
	rightContent := right.Content()
	if len(leftContent) != len(rightContent) {
		return false
	}
	for index := range leftContent {
		leftText, leftOK := leftContent[index].Text()
		rightText, rightOK := rightContent[index].Text()
		if leftOK != rightOK || !leftOK || leftText.Text() != rightText.Text() {
			return false
		}
	}
	return true
}

func assertChatStreamBackendError(t *testing.T, stream *chatCompletionsEventReader) {
	t.Helper()
	for {
		_, err := stream.Next(context.Background())
		if err == nil {
			continue
		}
		if err == io.EOF || !strings.Contains(err.Error(), "backend error") {
			t.Fatalf("stream error = %v, want backend-origin provider contradiction", err)
		}
		return
	}
}

func chatStreamFunctionRequest(t *testing.T) canonical.CanonicalRequest {
	return chatStreamFunctionRequests(t, "search")
}

func chatStreamFunctionRequests(t *testing.T, names ...string) canonical.CanonicalRequest {
	t.Helper()
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	declarations := make([]canonical.ToolDeclaration, 0, len(names))
	for _, name := range names {
		key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, name)
		declaration, _ := canonical.NewFunctionTool(key, "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
		declarations = append(declarations, declaration)
	}
	set, _ := canonical.NewToolSet(declarations)
	tools, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{tools}})
}

func chatStreamCustomRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindCustom, "shell")
	declaration, _ := canonical.NewCustomTool(key, "", canonical.EmptyToolFormat())
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	tools, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{tools}})
}

func chatStreamMixedToolRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	functionKey, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "search")
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	function, _ := canonical.NewFunctionTool(functionKey, "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool]())
	customKey, _ := canonical.NewRequestToolKey(canonical.ToolKindCustom, "shell")
	custom, _ := canonical.NewCustomTool(customKey, "", canonical.EmptyToolFormat())
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{function, custom})
	tools, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{tools}})
}
