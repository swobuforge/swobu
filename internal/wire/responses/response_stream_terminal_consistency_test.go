package responses

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseStreamRejectsProviderMCPBeforeSiblingPublication(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"mcp_call\",\"id\":\"mcp_1\",\"name\":\"Read\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.mcp_call_arguments.delta\ndata: {\"type\":\"response.mcp_call_arguments.delta\",\"output_index\":0,\"item_id\":\"mcp_1\",\"name\":\"Read\",\"delta\":\"one\"}\n\n" +
		"event: response.mcp_call_arguments.delta\ndata: {\"type\":\"response.mcp_call_arguments.delta\",\"output_index\":0,\"item_id\":\"mcp_1\",\"name\":\"Read\",\"delta\":\"two\"}\n\n" +
		"event: response.mcp_call_arguments.done\ndata: {\"type\":\"response.mcp_call_arguments.done\",\"output_index\":0,\"item_id\":\"mcp_1\",\"name\":\"Read\",\"arguments\":\"onetwo\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"mcp_call\",\"id\":\"mcp_1\",\"name\":\"Read\",\"status\":\"completed\",\"arguments\":\"onetwo\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}}\n\n" +
		responsesCompletedFrame("[]", "")
	stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
	assertResponsesBackendError(t, drainResponsesStream(stream))
}

func TestDecodeResponseStreamRetainsFirstCompletedItemAcrossTerminalContentMutation(t *testing.T) {
	tests := []struct {
		name     string
		request  canonical.CanonicalRequest
		doneItem string
		terminal string
	}{
		{
			name:     "message text",
			doneItem: `{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"hello"}]}`,
			terminal: `{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"goodbye"}]}`,
		},
		{
			name:     "message status",
			doneItem: `{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"hello"}]}`,
			terminal: `{"type":"message","id":"msg_1","status":"failed","content":[{"type":"output_text","text":"hello"}]}`,
		},
		{
			name:     "function arguments",
			request:  responsesFunctionRequest(t),
			doneItem: `{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","status":"completed","arguments":"{}"}`,
			terminal: `{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","status":"completed","arguments":"{\"x\":1}"}`,
		},
		{
			name:     "custom input",
			request:  responsesCustomRequest(t),
			doneItem: `{"type":"custom_tool_call","id":"ct_1","call_id":"call_1","name":"apply_patch","status":"completed","input":"hello"}`,
			terminal: `{"type":"custom_tool_call","id":"ct_1","call_id":"call_1","name":"apply_patch","status":"completed","input":"goodbye"}`,
		},
		{
			name:     "reasoning parts",
			doneItem: `{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"hello"}]}`,
			terminal: `{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"goodbye"}]}`,
		},
		{
			name:     "web search action",
			doneItem: `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","queries":["one"],"sources":[]}}`,
			terminal: `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","queries":["two"],"sources":[]}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			baseline := readResponsesStreamResponse(t, test.request, responsesCreatedFrame()+
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":"+test.doneItem+"}\n\n"+
				responsesCompletedFrame("[]", ""))
			mutated := readResponsesStreamResponse(t, test.request, responsesCreatedFrame()+
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":"+test.doneItem+"}\n\n"+
				responsesCompletedFrame("["+test.terminal+"]", ""))
			if !reflect.DeepEqual(mutated.Items(), baseline.Items()) {
				t.Fatalf("terminal mutation changed items: got %#v want %#v", mutated.Items(), baseline.Items())
			}
		})
	}
}

func TestDecodeResponseStreamAcceptsOpaqueReasoningCiphertextChange(t *testing.T) {
	doneItem := `{"type":"reasoning","id":"rs_1","status":"completed","encrypted_content":"item-done-cipher","summary":[{"type":"summary_text","text":"same reasoning"}]}`
	partial := `{"type":"reasoning","id":"rs_1","status":"incomplete","encrypted_content":"partial-cipher","summary":[{"type":"summary_text","text":"partial reasoning"}]}`
	tests := []struct {
		name       string
		raw        string
		wantCipher string
	}{
		{
			name: "completed terminal observation retains item done",
			raw: responsesCreatedFrame() +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + doneItem + "}\n\n" +
				responsesCompletedFrame(`[{"type":"reasoning","id":"rs_1","status":"completed","encrypted_content":"terminal-cipher","summary":[{"type":"summary_text","text":"same reasoning"}]}]`, ""),
			wantCipher: "item-done-cipher",
		},
		{
			name: "duplicate item done retains first item done",
			raw: responsesCreatedFrame() +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + doneItem + "}\n\n" +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs_1\",\"status\":\"banana\",\"encrypted_content\":\"duplicate-cipher\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"different reasoning\"}]}}\n\n" +
				responsesCompletedFrame("["+doneItem+"]", ""),
			wantCipher: "item-done-cipher",
		},
		{
			name: "deferred incomplete retains item done observation",
			raw: responsesCreatedFrame() +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + partial + "}\n\n" +
				"event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"incomplete\",\"output\":[{\"type\":\"reasoning\",\"id\":\"rs_1\",\"status\":\"incomplete\",\"encrypted_content\":\"terminal-cipher\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"partial reasoning\"}]}]}}\n\n",
			wantCipher: "partial-cipher",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, test.raw)
			reasoning, _ := response.Items()[0].Reasoning()
			replay, _ := reasoning.Opaque().Responses()
			if replay.EncryptedContent != test.wantCipher {
				t.Fatalf("replay = %#v, want cipher %q", replay, test.wantCipher)
			}
		})
	}
}

func TestDecodeResponseStreamHandlesDuplicateDeferredItemDoneByIdentity(t *testing.T) {
	first := `{"type":"reasoning","id":"rs_1","status":"incomplete","encrypted_content":"first-cipher","summary":[{"type":"summary_text","text":"first summary"}]}`
	terminal := "event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"incomplete\",\"output\":[]}}\n\n"

	t.Run("same identity ignores duplicate content before admission", func(t *testing.T) {
		duplicate := `{"type":"reasoning","id":"rs_1","status":"banana","encrypted_content":"second-cipher","summary":[{"type":"summary_text","text":"second summary"}]}`
		response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, responsesCreatedFrame()+
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":"+first+"}\n\n"+
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":"+duplicate+"}\n\n"+
			terminal)
		reasoning, ok := response.Items()[0].Reasoning()
		if !ok || len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Text() != "first summary" {
			t.Fatalf("reasoning = %#v, want first item-done summary", response.Items()[0])
		}
		replay, ok := reasoning.Opaque().Responses()
		if !ok || replay.EncryptedContent != "first-cipher" {
			t.Fatalf("replay = %#v, want first item-done cipher", replay)
		}
	})

	t.Run("changed identity still fails", func(t *testing.T) {
		duplicate := `{"type":"reasoning","id":"rs_2","status":"banana","encrypted_content":"second-cipher","summary":[{"type":"summary_text","text":"second summary"}]}`
		assertResponsesDecoderBackendError(t, canonical.CanonicalRequest{}, responsesCreatedFrame()+
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":"+first+"}\n\n"+
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":"+duplicate+"}\n\n"+
			terminal)
	})
}

func TestDecodeResponseStreamIgnoresTerminalReasoningReplayPresenceMutation(t *testing.T) {
	tests := []struct {
		name     string
		done     string
		terminal string
	}{
		{name: "present to absent", done: `,"encrypted_content":"cipher"`},
		{name: "absent to present", terminal: `,"encrypted_content":"cipher"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			done := `{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"same reasoning"}]` + test.done + `}`
			terminal := `{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"same reasoning"}]` + test.terminal + `}`
			response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, responsesCreatedFrame()+
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":"+done+"}\n\n"+
				responsesCompletedFrame("["+terminal+"]", ""))
			reasoning, _ := response.Items()[0].Reasoning()
			_, replayPresent := reasoning.Opaque().Responses()
			if replayPresent != (test.done != "") {
				t.Fatalf("replay present = %t, want item-done presence %t", replayPresent, test.done != "")
			}
		})
	}
}

func TestDecodeResponseStreamIgnoresCompatibilityEvidenceFromTerminalDuplicate(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"kept\"}]}}\n\n" +
		responsesCompletedFrame(`[{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"kept"},{"type":"future_content"}]}]`, "")
	stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
	assertResponsesProviderOutputItems(t, stream, 1)
	assertResponsesStreamDrop(t, stream, 0)
}

func TestDecodeResponseStreamRejectsTerminalDuplicateIdentityMutation(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"kept\"}]}}\n\n" +
		responsesCompletedFrame(`[{"type":"message","id":"msg_2","status":"completed","content":[{"type":"output_text","text":"ignored"}]}]`, "")
	assertResponsesDecoderBackendError(t, canonical.CanonicalRequest{}, raw)
}

func TestDecodeResponseStreamReconcilesOutOfOrderReasoningIndexes(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"item_id\":\"rs_1\",\"summary_index\":1,\"delta\":\"kept\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs_1\",\"status\":\"completed\",\"summary\":[{\"type\":\"future_summary\"},{\"type\":\"summary_text\",\"text\":\"kept\"}]}}\n\n" +
		responsesCompletedFrame("[]", "")
	stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "swobu_1"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	reasoning, ok := response.Items()[0].Reasoning()
	if !ok || len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Text() != "kept" {
		t.Fatalf("reasoning = %#v, want compact kept summary", response.Items()[0])
	}
	assertResponsesStreamDrop(t, stream, 1)
}

func TestDecodeResponseStreamRejectsAggregateOutputTextAcrossContentGap(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":1,\"delta\":\"hel\"}\n\n" +
		responsesCompletedFrame("[]", "hello")
	assertResponsesDecoderBackendError(t, canonical.CanonicalRequest{}, raw)
}

func TestResponsesUnknownWireEventsDoNotCreateSemanticEvidence(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.future.delta\ndata: {\"type\":\"response.future.delta\",\"output_index\":0,\"item_id\":\"shared\"}\n\n" +
		"event: response.future.delta\ndata: {\"type\":\"response.future.delta\",\"output_index\":1,\"item_id\":\"shared\"}\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
	for {
		if _, err := stream.Next(context.Background()); err != nil {
			break
		}
	}
	if len(stream.Changes()) != 0 {
		t.Fatalf("wire-only events created semantic changes: %#v", stream.Changes())
	}
}

func TestDecodeResponseStreamRejectsMissingCompleteItemType(t *testing.T) {
	for _, eventType := range []string{"response.output_item.added", "response.output_item.done"} {
		t.Run(eventType, func(t *testing.T) {
			raw := responsesCreatedFrame() +
				"event: " + eventType + "\ndata: {\"type\":\"" + eventType + "\",\"output_index\":0,\"item\":{\"id\":\"broken\",\"content\":[{\"type\":\"output_text\",\"text\":\"hidden\"}]}}\n\n" +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"visible\"}]}}\n\n" +
				responsesCompletedFrame("[]", "")
			assertResponsesDecoderBackendError(t, canonical.CanonicalRequest{}, raw)
		})
	}
}

func TestDecodeResponseStreamRejectsActiveStatusAtOutputItemDone(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"in_progress\",\"content\":[{\"type\":\"output_text\",\"text\":\"partial\"}]}}\n\n" +
		responsesCompletedFrame("[]", "")
	assertResponsesDecoderBackendError(t, canonical.CanonicalRequest{}, raw)
}

func TestDecodeResponseStreamAdmitsEveryCompleteItemBeforeRecovery(t *testing.T) {
	tests := []struct {
		name    string
		request canonical.CanonicalRequest
		frames  string
		output  string
	}{
		{
			name:   "progressive message terminal active",
			frames: "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"delta\":\"partial\"}\n\n",
			output: `[{"type":"message","id":"msg_1","status":"in_progress","content":[{"type":"output_text","text":"partial"}]}]`,
		},
		{
			name:    "progressive tool terminal active",
			request: responsesFunctionRequest(t),
			frames:  "event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"delta\":\"{}\"}\n\n",
			output:  `[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","status":"in_progress","arguments":"{}"}]`,
		},
		{
			name:   "unfamiliar known status",
			frames: "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"banana\",\"content\":[{\"type\":\"output_text\",\"text\":\"invalid\"}]}}\n\n",
			output: "[]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertResponsesDecoderBackendError(
				t,
				test.request,
				responsesCreatedFrame()+test.frames+responsesCompletedFrame(test.output, ""),
			)
		})
	}
}

func TestDecodeResponseStreamAcceptsValidTerminalItemStatus(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"valid\"}]}}\n\n" +
		responsesCompletedFrame("[]", "")
	stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
	assertResponsesProviderOutputItems(t, stream, 1)
}

func TestDecodeResponseStreamDefersPartialItemUntilIncompleteResponse(t *testing.T) {
	item := `{"type":"message","id":"msg_1","status":"incomplete","content":[{"type":"output_text","text":"partial"}]}`
	incomplete := "event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"incomplete\",\"output\":[]}}\n\n"
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + item + "}\n\n" +
		incomplete
	response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, raw)
	if response.Completion().Reason() != "incomplete" || len(response.Items()) != 1 {
		t.Fatalf("partial response reason=%q items=%#v", response.Completion().Reason(), response.Items())
	}
	message, ok := response.Items()[0].Message()
	if !ok {
		t.Fatalf("partial item = %#v, want message", response.Items()[0])
	}
	text, ok := message.Content()[0].Text()
	if !ok || text.Text() != "partial" {
		t.Fatalf("partial message = %#v", message.Content())
	}
}

func TestDecodeResponseStreamRejectsPartialItemUnderCompletedResponse(t *testing.T) {
	item := `{"type":"message","id":"msg_1","status":"incomplete","content":[{"type":"output_text","text":"partial"}]}`
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + item + "}\n\n" +
		responsesCompletedFrame("[]", "")
	assertResponsesDecoderBackendError(t, canonical.CanonicalRequest{}, raw)
}

func TestDecodeResponseStreamRejectsLifecycleAfterDeferredDoneBeforeEmittingItem(t *testing.T) {
	tests := []struct {
		name  string
		item  string
		frame string
	}{
		{
			name:  "message delta",
			item:  `{"type":"message","id":"msg_1","status":"incomplete","content":[{"type":"output_text","text":"partial"}]}`,
			frame: "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"delta\":\"later\"}\n\n",
		},
		{
			name:  "reasoning delta",
			item:  `{"type":"reasoning","id":"rs_1","status":"incomplete","summary":[{"type":"summary_text","text":"partial"}]}`,
			frame: "event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"item_id\":\"rs_1\",\"summary_index\":0,\"delta\":\"later\"}\n\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := responsesCreatedFrame() +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + test.item + "}\n\n" +
				test.frame
			stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
			itemEvents := 0
			for {
				event, err := stream.Next(context.Background())
				if err != nil {
					if !strings.Contains(err.Error(), "backend error from responses") {
						t.Fatalf("stream error = %v, want backend-origin lifecycle error", err)
					}
					break
				}
				if _, ok := event.Payload.(canonical.ItemEvent); ok {
					itemEvents++
				}
			}
			if itemEvents != 0 {
				t.Fatalf("item-scoped events before deferred lifecycle error = %d, want 0", itemEvents)
			}
		})
	}
}

func TestDecodeResponseStreamRecordsDiscoveryChildErasureAtDone(t *testing.T) {
	request := responsesDiscoveryRequest(t)
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"tool_search_output\",\"call_id\":\"search_1\",\"status\":\"completed\",\"execution\":\"server\",\"tools\":[{\"type\":\"future_tool\"},{\"type\":\"function\",\"name\":\"kept\",\"parameters\":{}}]}}\n\n" +
		responsesCompletedFrame("[]", "")
	stream := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
	completed := 0
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			break
		}
		if event.Kind != canonical.EventItemCompleted {
			continue
		}
		itemEvent, ok := event.Payload.(canonical.ItemEvent)
		if !ok {
			t.Fatalf("completed event payload = %#v", event.Payload)
		}
		payload, ok := itemEvent.Payload.(canonical.ItemCompletedPayload)
		if !ok || payload.Item.Kind() != canonical.ItemKindToolDiscoveryResult {
			t.Fatalf("completed output = %#v, want discovery result", itemEvent.Payload)
		}
		completed++
	}
	if completed != 1 {
		t.Fatalf("completed discovery results = %d, want 1", completed)
	}
	assertResponsesStreamDrop(t, stream, 1)
}

func TestDecodeResponseStreamDeduplicatesRepeatedChildErasureEvidence(t *testing.T) {
	item := `{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"kept"},{"type":"future_content"}]}`
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + item + "}\n\n" +
		responsesCompletedFrame("["+item+"]", "")
	stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
	assertResponsesProviderOutputItems(t, stream, 1)
	assertResponsesStreamDrop(t, stream, 1)
}

func TestDecodeResponseStreamRejectsAllErasedDiscoveryResult(t *testing.T) {
	call := `{"type":"tool_search_call","call_id":"search_1","status":"completed","execution":"server","arguments":"{}"}`
	result := `{"type":"tool_search_output","call_id":"search_1","status":"completed","execution":"server","tools":[{"type":"future_tool"}]}`
	tests := []struct {
		name   string
		frames string
		output string
	}{
		{
			name: "output item done",
			frames: "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + call + "}\n\n" +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":" + result + "}\n\n",
			output: "[]",
		},
		{
			name:   "terminal fallback",
			output: "[" + call + "," + result + "]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertResponsesDecoderBackendError(
				t,
				responsesDiscoveryRequest(t),
				responsesCreatedFrame()+test.frames+responsesCompletedFrame(test.output, ""),
			)
		})
	}
}

func responsesDiscoveryRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	schema, err := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	discovery, err := canonical.NewToolDiscoveryTool("find tools", canonical.NewToolSchemaObject(schema), canonical.DiscoveryExecutorProvider)
	if err != nil {
		t.Fatal(err)
	}
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{discovery})
	if err != nil {
		t.Fatal(err)
	}
	declarations, err := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	if err != nil {
		t.Fatal(err)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{declarations}})
}
