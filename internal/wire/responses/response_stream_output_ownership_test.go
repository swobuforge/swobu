package responses

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseStreamCorrelatesProgressiveStateByOutputIndex(t *testing.T) {
	t.Run("tool item id appears at terminal", func(t *testing.T) {
		raw := responsesCreatedFrame() +
			"event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"call_id\":\"call_1\",\"name\":\"lookup\",\"delta\":\"{}\"}\n\n" +
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"status\":\"completed\",\"arguments\":\"{}\"}}\n\n" +
			responsesCompletedFrame("[]", "")
		response := readResponsesStreamResponse(t, responsesFunctionRequest(t), raw)
		if len(response.Items()) != 1 {
			t.Fatalf("items = %#v, want one tool call", response.Items())
		}
		call, ok := response.Items()[0].ToolCall()
		if !ok || call.CallID().String() != "call_1" {
			t.Fatalf("item = %#v, want call_1", response.Items()[0])
		}
	})

	t.Run("reasoning item id appears at terminal", func(t *testing.T) {
		raw := responsesCreatedFrame() +
			"event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"summary_index\":0,\"delta\":\"hel\"}\n\n" +
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs_1\",\"status\":\"completed\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"hello\"}]}}\n\n" +
			responsesCompletedFrame("[]", "")
		response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, raw)
		if len(response.Items()) != 1 || response.Items()[0].Kind() != canonical.ItemKindReasoning {
			t.Fatalf("items = %#v, want one reasoning item", response.Items())
		}
	})
}

func TestDecodeResponseStreamKeepsAnonymousReasoningIndexLocal(t *testing.T) {
	t.Run("reasoning without item ids", func(t *testing.T) {
		raw := responsesCreatedFrame() +
			"event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":1,\"summary_index\":0,\"delta\":\"sec\"}\n\n" +
			"event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"summary_index\":0,\"delta\":\"fir\"}\n\n" +
			responsesCompletedFrame(`[{"type":"reasoning","status":"completed","summary":[{"type":"summary_text","text":"first"}]},{"type":"reasoning","status":"completed","summary":[{"type":"summary_text","text":"second"}]}]`, "")
		response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, raw)
		if len(response.Items()) != 2 {
			t.Fatalf("items = %#v, want two reasoning items", response.Items())
		}
	})

}

func TestDecodeResponseStreamRejectsDuplicatePendingToolCallIDs(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":1,\"item_id\":\"fc_shared\",\"call_id\":\"call_shared\",\"name\":\"lookup\",\"delta\":\"{\\\"x\\\":2}\"}\n\n" +
		"event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"fc_shared\",\"call_id\":\"call_shared\",\"name\":\"lookup\",\"delta\":\"{\\\"x\\\":1}\"}\n\n" +
		responsesCompletedFrame(`[{"type":"function_call","id":"fc_shared","call_id":"call_shared","name":"lookup","status":"completed","arguments":"{\"x\":1}"},{"type":"function_call","id":"fc_shared","call_id":"call_shared","name":"lookup","status":"completed","arguments":"{\"x\":2}"}]`, "")
	stream := decodeResponseStream(responsesFunctionRequest(t), testAttemptToolNames(responsesFunctionRequest(t)), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "swobu_1"})
	_, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err == nil {
		t.Fatal("duplicate pending function call IDs reached a valid canonical response")
	}
	if !strings.Contains(err.Error(), "tool call repeats a pending id") {
		t.Fatalf("stream error = %v, want duplicate pending call rejection", err)
	}
}

func TestDecodeResponseStreamFreezesErasedOutputLifecycle(t *testing.T) {
	t.Run("unknown added then known done", func(t *testing.T) {
		raw := responsesCreatedFrame() +
			"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"\"}}\n\n" +
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"late\"}]}}\n\n"
		assertResponsesDecoderBackendError(t, canonical.CanonicalRequest{}, raw)
	})

	t.Run("matching unknown done erases once", func(t *testing.T) {
		raw := responsesCreatedFrame() +
			"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"future_output\"}}\n\n" +
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"future_output\"}}\n\n" +
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"kept\"}]}}\n\n" +
			responsesCompletedFrame("[]", "")
		stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
		assertResponsesProviderOutputItems(t, stream, 1)
		assertResponsesStreamDrop(t, stream, 1)
	})

	t.Run("resolved erasure cannot change span or emit", func(t *testing.T) {
		stream := &responsesResponseStream{providerOutputs: map[int]*pendingResponseOutput{}}
		outputIndex := 0
		unknownDone := streamFrame{Type: "response.output_item.done", OutputIndex: &outputIndex}
		unknownDone.Item.Type = "future_output"
		unknownDone.Item.ID = "future_1"
		if handled, _, err := stream.handleFrame(context.Background(), unknownDone); err != nil || !handled {
			t.Fatalf("unknown done handled=%v err=%v", handled, err)
		}
		state := stream.outputAt(0)
		beforeEvents := len(stream.pending)
		beforeSpan := state.span
		if handled, _, err := stream.handleFrame(context.Background(), unknownDone); err != nil || !handled {
			t.Fatalf("duplicate unknown done handled=%v err=%v", handled, err)
		}
		if state.span != beforeSpan || len(stream.pending) != beforeEvents {
			t.Fatalf("duplicate changed span/events: span %d -> %d, events %d -> %d", beforeSpan, state.span, beforeEvents, len(stream.pending))
		}
		knownDone := streamFrame{Type: "response.output_item.done", OutputIndex: &outputIndex}
		knownDone.Item.Type = "message"
		knownDone.Item.ID = "msg_1"
		if _, _, err := stream.handleFrame(context.Background(), knownDone); err == nil {
			t.Fatal("resolved erasure accepted known output")
		}
		if state.span != beforeSpan || len(stream.pending) != beforeEvents {
			t.Fatalf("contradiction changed span/events: span %d -> %d, events %d -> %d", beforeSpan, state.span, beforeEvents, len(stream.pending))
		}
	})
}

func TestDecodeResponseStreamOrdersMessagePartsByContentIndex(t *testing.T) {
	tests := map[string]string{
		"one before zero": responsesCreatedFrame() +
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":1,\"delta\":\"second\"}\n\n" +
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":0,\"delta\":\"first\"}\n\n" +
			responsesCompletedFrame(`[{"type":"message","id":"msg_0","status":"completed","content":[{"type":"output_text","text":"first"},{"type":"output_text","text":"second"}]}]`, ""),
		"terminal zero before streamed one": responsesCreatedFrame() +
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":1,\"delta\":\"second\"}\n\n" +
			responsesCompletedFrame(`[{"type":"message","id":"msg_0","status":"completed","content":[{"type":"output_text","text":"first"},{"type":"output_text","text":"second"}]}]`, ""),
		"two zero one arrival": responsesCreatedFrame() +
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":2,\"delta\":\"third\"}\n\n" +
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":0,\"delta\":\"first\"}\n\n" +
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":1,\"delta\":\"second\"}\n\n" +
			responsesCompletedFrame(`[{"type":"message","id":"msg_0","status":"completed","content":[{"type":"output_text","text":"first"},{"type":"output_text","text":"second"},{"type":"output_text","text":"third"}]}]`, ""),
		"known unknown known": responsesCreatedFrame() +
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":2,\"delta\":\"third\"}\n\n" +
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":0,\"delta\":\"first\"}\n\n" +
			responsesCompletedFrame(`[{"type":"message","id":"msg_0","status":"completed","content":[{"type":"output_text","text":"first"},{"type":"future_content"},{"type":"output_text","text":"third"}]}]`, ""),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, raw)
			message, ok := response.Items()[0].Message()
			if !ok {
				t.Fatalf("item = %#v, want message", response.Items()[0])
			}
			want := []string{"first", "second"}
			if name == "two zero one arrival" {
				want = []string{"first", "second", "third"}
			}
			if name == "known unknown known" {
				want = []string{"first", "third"}
			}
			if len(message.Content()) != len(want) {
				t.Fatalf("content = %#v, want %d parts", message.Content(), len(want))
			}
			for index, wantText := range want {
				text, _ := message.Content()[index].Text()
				if text.Text() != wantText {
					t.Fatalf("part %d = %q, want %q", index, text.Text(), wantText)
				}
			}
		})
	}
}

func TestDecodeResponseStreamRejectsOutputScopedFramesWithoutIndex(t *testing.T) {
	tests := map[string]struct {
		request canonical.CanonicalRequest
		frame   string
	}{
		"text": {
			frame: "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"content_index\":0,\"delta\":\"text\"}\n\n",
		},
		"tool": {
			request: responsesFunctionRequest(t),
			frame:   "event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"delta\":\"{}\"}\n\n",
		},
		"reasoning": {
			frame: "event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"item_id\":\"rs_1\",\"summary_index\":0,\"delta\":\"thought\"}\n\n",
		},
		"anonymous then indexed terminal": {
			frame: "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"content_index\":0,\"delta\":\"text\"}\n\n" +
				responsesCompletedFrame(`[{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"text"}]}]`, ""),
		},
		"two anonymous identities": {
			frame: "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"content_index\":0,\"delta\":\"one\"}\n\n" +
				"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_2\",\"content_index\":0,\"delta\":\"two\"}\n\n",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assertResponsesDecoderBackendError(t, test.request, responsesCreatedFrame()+test.frame)
		})
	}
}

func TestDecodeResponseStreamRecordsErasurePerOutputIndex(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"future_output\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"future_output\"}}\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
	for {
		_, err := stream.Next(context.Background())
		if err != nil {
			break
		}
	}
	assertResponsesStreamDrop(t, stream, 2)
}
