package responses

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestDecodeResponseStreamReconcilesPartialMessageWithDoneAndTerminalSnapshots(t *testing.T) {
	for _, terminalOnly := range []bool{false, true} {
		name := "output_item_done"
		if terminalOnly {
			name = "response_completed"
		}
		t.Run(name, func(t *testing.T) {
			middle := "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n\n"
			output := "[]"
			if terminalOnly {
				middle = ""
				output = "[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}]"
			}
			raw := responsesCreatedFrame() +
				"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hel\"}\n\n" +
				middle +
				responsesCompletedFrame(output, "")
			response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, raw)
			message, ok := response.Items()[0].Message()
			if !ok {
				t.Fatalf("output item = %#v, want message", response.Items()[0])
			}
			text, _ := message.Content()[0].Text()
			if text.Text() != "hello" {
				t.Fatalf("message text = %q, want hello", text.Text())
			}
		})
	}
}

func TestDecodeResponseStreamReconcilesPartialFunctionInputWithDoneAndTerminalSnapshots(t *testing.T) {
	request := responsesFunctionRequest(t)
	for _, terminalOnly := range []bool{false, true} {
		name := "output_item_done"
		if terminalOnly {
			name = "response_completed"
		}
		t.Run(name, func(t *testing.T) {
			middle := "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"status\":\"completed\",\"arguments\":\"{\\\"x\\\":1}\"}}\n\n"
			output := "[]"
			if terminalOnly {
				middle = ""
				output = "[{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"status\":\"completed\",\"arguments\":\"{\\\"x\\\":1}\"}]"
			}
			raw := responsesCreatedFrame() +
				"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"status\":\"in_progress\"}}\n\n" +
				"event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"delta\":\"{\\\"x\\\":\"}\n\n" +
				middle +
				responsesCompletedFrame(output, "")
			response := readResponsesStreamResponse(t, request, raw)
			call, ok := response.Items()[0].ToolCall()
			if !ok {
				t.Fatalf("output item = %#v, want tool call", response.Items()[0])
			}
			object, _ := call.Input().Object()
			if object.String() != `{"x":1}` {
				t.Fatalf("tool input = %s, want full terminal object", object.String())
			}
		})
	}
}

func TestDecodeResponseStreamReconcilesPartialCustomInputWithDoneAndTerminalSnapshots(t *testing.T) {
	request := responsesCustomRequest(t)
	for _, terminalOnly := range []bool{false, true} {
		name := "output_item_done"
		if terminalOnly {
			name = "response_completed"
		}
		t.Run(name, func(t *testing.T) {
			middle := "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"custom_tool_call\",\"id\":\"custom_1\",\"call_id\":\"call_1\",\"name\":\"apply_patch\",\"status\":\"completed\",\"input\":\"hello\"}}\n\n"
			output := "[]"
			if terminalOnly {
				middle = ""
				output = "[{\"type\":\"custom_tool_call\",\"id\":\"custom_1\",\"call_id\":\"call_1\",\"name\":\"apply_patch\",\"status\":\"completed\",\"input\":\"hello\"}]"
			}
			raw := responsesCreatedFrame() +
				"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"custom_tool_call\",\"id\":\"custom_1\",\"call_id\":\"call_1\",\"name\":\"apply_patch\",\"status\":\"in_progress\"}}\n\n" +
				"event: response.custom_tool_call_input.delta\ndata: {\"type\":\"response.custom_tool_call_input.delta\",\"output_index\":0,\"item_id\":\"custom_1\",\"call_id\":\"call_1\",\"name\":\"apply_patch\",\"delta\":\"hel\"}\n\n" +
				middle +
				responsesCompletedFrame(output, "")
			response := readResponsesStreamResponse(t, request, raw)
			call, ok := response.Items()[0].ToolCall()
			if !ok {
				t.Fatalf("output item = %#v, want custom tool call", response.Items()[0])
			}
			input, _ := call.Input().Text()
			if input != "hello" {
				t.Fatalf("custom input = %q, want hello", input)
			}
		})
	}
}

func TestDecodeResponseStreamRejectsContradictoryDoneAndTerminalSnapshots(t *testing.T) {
	for _, terminalOnly := range []bool{false, true} {
		name := "output_item_done"
		if terminalOnly {
			name = "response_completed"
		}
		t.Run(name, func(t *testing.T) {
			middle := "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"world\"}]}}\n\n"
			output := "[]"
			if terminalOnly {
				middle = ""
				output = "[{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"world\"}]}]"
			}
			raw := responsesCreatedFrame() +
				"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hello\"}\n\n" +
				middle +
				responsesCompletedFrame(output, "")
			assertResponsesStreamFails(t, canonical.CanonicalRequest{}, raw)
		})
	}
}

func TestDecodeResponseStreamRejectsTerminalFunctionInputThatRewritesCompletedProgress(t *testing.T) {
	request := responsesFunctionRequest(t)
	for _, terminalOnly := range []bool{false, true} {
		name := "output_item_done"
		if terminalOnly {
			name = "response_completed"
		}
		t.Run(name, func(t *testing.T) {
			middle := "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"status\":\"completed\",\"arguments\":\"{\\\"x\\\":1}\"}}\n\n"
			output := "[]"
			if terminalOnly {
				middle = ""
				output = "[{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"status\":\"completed\",\"arguments\":\"{\\\"x\\\":1}\"}]"
			}
			raw := responsesCreatedFrame() +
				"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\"}}\n\n" +
				"event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"delta\":\"{}\"}\n\n" +
				middle +
				responsesCompletedFrame(output, "")
			assertResponsesStreamFails(t, request, raw)
		})
	}
}

func TestDecodeResponseStreamRejectsUnfinishedKnownOutput(t *testing.T) {
	tests := map[string]struct {
		request canonical.CanonicalRequest
		frame   string
	}{
		"added function call": {
			request: responsesFunctionRequest(t),
			frame:   "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\"}}\n\n",
		},
		"function arguments delta": {
			request: responsesFunctionRequest(t),
			frame:   "event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"delta\":\"{}\"}\n\n",
		},
		"added reasoning": {
			frame: "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs_1\"}}\n\n",
		},
		"reasoning delta": {
			frame: "event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"item_id\":\"rs_1\",\"summary_index\":0,\"delta\":\"partial\"}\n\n",
		},
		"added message": {
			frame: "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\"}}\n\n",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assertResponsesStreamFails(t, test.request, responsesCreatedFrame()+test.frame+responsesCompletedFrame("[]", ""))
		})
	}
}

func TestDecodeResponseStreamRejectsPartialTextWithoutCheckpoint(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hel\"}\n\n" +
		responsesCompletedFrame("[]", "")
	assertResponsesStreamFails(t, canonical.CanonicalRequest{}, raw)
}

func TestDecodeResponseStreamRetainsPartialTextAcrossLaterToolStart(t *testing.T) {
	request := responsesFunctionRequest(t)
	raw := responsesCreatedFrame() +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hel\"}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"{}\"}}\n\n" +
		responsesCompletedFrame(
			`[{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"hello"}]},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}]`,
			"",
		)
	response := readResponsesStreamResponse(t, request, raw)
	items := response.Items()
	if len(items) != 2 {
		t.Fatalf("canonical output items = %#v, want message and tool call", items)
	}
	message, _ := items[0].Message()
	text, _ := message.Content()[0].Text()
	if text.Text() != "hello" {
		t.Fatalf("message text = %q, want terminal checkpoint hello", text.Text())
	}
	call, ok := items[1].ToolCall()
	if !ok {
		t.Fatalf("second output item = %#v, want tool call", items[1])
	}
	if call.CallID().String() != "call_1" || call.Tool().Name() != "lookup" {
		t.Fatalf("tool call = %#v, want call_1/lookup", call)
	}
	object, ok := call.Input().Object()
	if !ok || object.String() != `{}` {
		t.Fatalf("tool input = %#v, want {}", call.Input())
	}
}

func TestDecodeResponseStreamRejectsOutputIdentityMutation(t *testing.T) {
	tests := []struct {
		name    string
		request canonical.CanonicalRequest
		first   string
		second  string
	}{
		{
			name:   "message item id",
			first:  "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_a\",\"content_index\":0,\"delta\":\"hel\"}\n\n",
			second: "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_b\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n\n",
		},
		{
			name:    "tool call id",
			request: responsesFunctionRequest(t),
			first:   "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_a\",\"name\":\"lookup\"}}\n\n",
			second:  "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_b\",\"name\":\"lookup\",\"arguments\":\"{}\"}}\n\n",
		},
		{
			name:    "tool name",
			request: responsesFunctionRequest(t),
			first:   "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\"}}\n\n",
			second:  "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"delete\",\"arguments\":\"{}\"}}\n\n",
		},
		{
			name:    "tool kind",
			request: responsesFunctionRequest(t),
			first:   "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\"}}\n\n",
			second:  "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"custom_tool_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"input\":\"{}\"}}\n\n",
		},
		{
			name:   "reasoning item id",
			first:  "event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"item_id\":\"rs_a\",\"summary_index\":0,\"delta\":\"hel\"}\n\n",
			second: "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs_b\",\"status\":\"completed\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"hello\"}]}}\n\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertResponsesStreamFailsBeforeCompletion(t, test.request, responsesCreatedFrame()+test.first+test.second)
		})
	}
}

func TestDecodeResponseStreamRejectsLifecycleAfterTerminalOutputIndex(t *testing.T) {
	tests := map[string]string{
		"late delta": "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}}\n\n" +
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"delta\":\"late\"}\n\n",
		"known after erased": "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"future_output\",\"id\":\"future_1\"}}\n\n" +
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"late\"}]}}\n\n",
	}
	for name, lifecycle := range tests {
		t.Run(name, func(t *testing.T) {
			assertResponsesDecoderBackendError(t, canonical.CanonicalRequest{}, responsesCreatedFrame()+lifecycle)
		})
	}
}

func TestDecodeResponseStreamOrdersOutOfOrderProviderIndexes(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"second\"}]}}\n\n" +
		responsesCompletedFrame(
			`[{"type":"message","id":"msg_0","status":"completed","content":[{"type":"output_text","text":"first"}]},{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"second"}]}]`,
			"",
		)
	response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, raw)
	assertResponsesMessageTexts(t, response.Items(), "first", "second")
}

func TestDecodeResponseStreamCompactsEarlierErasedProviderIndex(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"second\"}]}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"future_output\",\"id\":\"future_0\"}}\n\n" +
		responsesCompletedFrame("[]", "")
	response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, raw)
	assertResponsesMessageTexts(t, response.Items(), "second")
}

func TestDecodeResponseStreamDoesNotCompletePastUnresolvedFrontier(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":2,\"item\":{\"type\":\"message\",\"id\":\"msg_2\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"third\"}]}}\n\n" +
		responsesCompletedFrame("[]", "")
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			if !strings.Contains(err.Error(), "backend error from responses") {
				t.Fatalf("stream error = %v, want unresolved-frontier backend error", err)
			}
			return
		}
		if event.Kind == canonical.EventItemCompleted {
			t.Fatal("index 2 completed before indexes 0 and 1 resolved")
		}
	}
}

func TestDecodeResponseStreamCompactsExpandedAndErasedIndexes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "erased before expansion",
			raw: "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"web_search_call\",\"id\":\"ws_1\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"queries\":[\"q\"],\"sources\":[]}}}\n\n" +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"future_output\",\"id\":\"future_0\"}}\n\n",
		},
		{
			name: "erased after expansion",
			raw: "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"web_search_call\",\"id\":\"ws_0\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"queries\":[\"q\"],\"sources\":[]}}}\n\n" +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"future_output\",\"id\":\"future_1\"}}\n\n" +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":2,\"item\":{\"type\":\"message\",\"id\":\"msg_2\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"answer\"}]}}\n\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, responsesCreatedFrame()+test.raw+responsesCompletedFrame("[]", ""))
			items := response.Items()
			want := 2
			if test.name == "erased after expansion" {
				want = 3
			}
			if len(items) != want {
				t.Fatalf("canonical items = %#v, want %d", items, want)
			}
			if items[0].Kind() != canonical.ItemKindToolCall || items[1].Kind() != canonical.ItemKindToolResult {
				t.Fatalf("expanded web-search items = %#v", items[:2])
			}
			if want == 3 {
				assertResponsesMessageTexts(t, items[2:], "answer")
			}
		})
	}
}

func TestDecodeResponseStreamPreservesMultipleMessageContentParts(t *testing.T) {
	tests := []struct {
		name   string
		frames string
		output string
		want   []string
	}{
		{
			name: "two streamed parts",
			frames: "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":0,\"delta\":\"first\"}\n\n" +
				"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":1,\"delta\":\"second\"}\n\n",
			output: `[{"type":"message","id":"msg_0","status":"completed","content":[{"type":"output_text","text":"first"},{"type":"output_text","text":"second"}]}]`,
			want:   []string{"first", "second"},
		},
		{
			name:   "known unknown known terminal",
			output: `[{"type":"message","id":"msg_0","status":"completed","content":[{"type":"output_text","text":"before"},{"type":"future_content"},{"type":"output_text","text":"after"}]}]`,
			want:   []string{"before", "after"},
		},
		{
			name: "interleaved deltas",
			frames: "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":0,\"delta\":\"a\"}\n\n" +
				"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":1,\"delta\":\"b\"}\n\n" +
				"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":0,\"delta\":\"c\"}\n\n" +
				"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":1,\"delta\":\"d\"}\n\n",
			output: `[{"type":"message","id":"msg_0","status":"completed","content":[{"type":"output_text","text":"ac"},{"type":"output_text","text":"bd"}]}]`,
			want:   []string{"ac", "bd"},
		},
		{
			name:   "unknown before streamed known",
			frames: "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":1,\"delta\":\"after\"}\n\n",
			output: `[{"type":"message","id":"msg_0","status":"completed","content":[{"type":"future_content"},{"type":"output_text","text":"after"}]}]`,
			want:   []string{"after"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := responsesCreatedFrame() + test.frames + responsesCompletedFrame(test.output, "")
			response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, raw)
			message, ok := response.Items()[0].Message()
			if !ok {
				t.Fatalf("output = %#v, want message", response.Items())
			}
			content := message.Content()
			if len(content) != len(test.want) {
				t.Fatalf("message content = %#v, want %q", content, test.want)
			}
			for index, want := range test.want {
				text, ok := content[index].Text()
				if !ok || text.Text() != want {
					t.Fatalf("message part %d = %#v, want %q", index, content[index], want)
				}
			}
		})
	}
}

func TestDecodeResponseStreamRecordsUnknownPartBeforeStreamedKnownPart(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_0\",\"content_index\":1,\"delta\":\"after\"}\n\n" +
		responsesCompletedFrame(
			`[{"type":"message","id":"msg_0","status":"completed","content":[{"type":"future_content"},{"type":"output_text","text":"after"}]}]`,
			"",
		)
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "swobu_1"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	assertResponsesMessageTexts(t, response.Items(), "after")
	assertResponsesStreamDrop(t, stream, 1)
}

func assertResponsesStreamFailsBeforeCompletion(t *testing.T, request canonical.CanonicalRequest, raw string) {
	t.Helper()
	stream := decodeResponseStream(request, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	completed := 0
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			if !strings.Contains(err.Error(), "backend error from responses") {
				t.Fatalf("stream error = %v, want backend-origin identity error", err)
			}
			break
		}
		if event.Kind == canonical.EventItemCompleted {
			completed++
		}
	}
	if completed != 0 {
		t.Fatalf("completed items before identity error = %d, want 0", completed)
	}
}

func assertResponsesDecoderBackendError(t *testing.T, request canonical.CanonicalRequest, raw string) {
	t.Helper()
	stream := decodeResponseStream(request, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	for {
		_, err := stream.Next(context.Background())
		if err == nil {
			continue
		}
		if !strings.Contains(err.Error(), "backend error from responses") {
			t.Fatalf("stream error = %v, want backend-origin lifecycle error", err)
		}
		return
	}
}

func assertResponsesMessageTexts(t *testing.T, items []canonical.CanonicalItem, want ...string) {
	t.Helper()
	if len(items) != len(want) {
		t.Fatalf("canonical items = %#v, want %d messages", items, len(want))
	}
	for index, wantText := range want {
		message, ok := items[index].Message()
		if !ok || len(message.Content()) != 1 {
			t.Fatalf("item %d = %#v, want one-part message", index, items[index])
		}
		text, ok := message.Content()[0].Text()
		if !ok || text.Text() != wantText {
			t.Fatalf("item %d text = %#v, want %q", index, message.Content(), wantText)
		}
	}
}

func TestDecodeResponseStreamRejectsPartialTextAtDoneSentinel(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"hel\"}\n\n" +
		"data: [DONE]\n\n"
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "swobu_1"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err == nil {
		_, err = closed.ProjectResponse()
	}
	if err == nil {
		t.Fatal("partial text reached successful [DONE] completion")
	}
}

func TestDecodeResponseStreamRejectsErasedOnlyOutputAtDoneSentinel(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"future_output\",\"id\":\"future_1\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"future_output\",\"id\":\"future_1\",\"status\":\"completed\"}}\n\n" +
		"data: [DONE]\n\n"
	assertResponsesDecoderBackendError(t, canonical.CanonicalRequest{}, raw)
}

func TestDecodeResponseStreamProviderErrorRemainsBackendOrigin(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: error\ndata: {\"type\":\"error\",\"code\":\"server_error\",\"message\":\"provider unavailable\"}\n\n"
	assertResponsesDecoderBackendError(t, canonical.CanonicalRequest{}, raw)
}

func TestDecodeResponseStreamPreservesWebSearchCallWhenStatusIsUnknown(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"web_search_call\",\"id\":\"ws_1\",\"status\":\"future_status\",\"action\":{\"type\":\"search\",\"queries\":[\"q\"]}}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"kept\"}]}}\n\n" +
		responsesCompletedFrame("[]", "")
	changeLog := &recordingChanges{}
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", changeLog)
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_1", TargetID: "target", TargetVersion: 1})
	envelope, err := canonical.ReadClosedEnvelope(context.Background(), bound, canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	items := completedResponsesStreamItems(envelope.Events)
	if len(items) != 2 {
		t.Fatalf("completed stream items = %#v, want pending web-search call and message", items)
	}
	call, ok := items[0].ToolCall()
	if !ok || call.CallID().String() != "ws_1" {
		t.Fatalf("first completed item = %#v, want preserved ws_1 call", items[0])
	}
	if _, ok := items[1].Message(); !ok {
		t.Fatalf("second completed item = %#v, want surviving message", items[1])
	}
	if _, err := envelope.ProjectResponse(); err == nil {
		t.Fatal("completed stream projected an unsettled web-search call")
	}
	drops := 0
	for _, decision := range stream.Changes() {
		if decision.Capability == canonical.ResponseItemsKind && decision.Kind == compat.Omission {
			drops++
			item, ok := decision.Occurrence.ResponseItem()
			if !ok || item != 0 {
				t.Fatalf("omission occurrence = %#v, want web-search item", decision.Occurrence)
			}
		}
	}
	if drops != 1 {
		t.Fatalf("changes = changeLog %#v stream %#v, want one Drop", *changeLog, stream.Changes())
	}
}

func TestDecodeResponseStreamUnknownWebSearchStatusFailsAtSettlement(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"web_search_call\",\"id\":\"ws_1\",\"status\":\"future_status\",\"action\":{\"type\":\"search\",\"queries\":[\"q\"]}}}\n\n" +
		responsesCompletedFrame("[]", "")
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_1", TargetID: "target", TargetVersion: 1})
	_, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err == nil || !strings.Contains(err.Error(), "web-search call has no provider result") {
		t.Fatalf("stream settlement error = %v, want unsettled web-search failure", err)
	}
	changes := stream.Changes()
	drops := 0
	for _, decision := range changes {
		if decision.Capability != canonical.ResponseItemsKind || decision.Kind != compat.Omission {
			continue
		}
		drops++
		item, ok := decision.Occurrence.ResponseItem()
		if !ok || item != 0 {
			t.Fatalf("omission occurrence = %#v, want web-search item", decision.Occurrence)
		}
	}
	if drops != 1 {
		t.Fatalf("changes = %#v, want occurrence-local status Drop", changes)
	}
}

func completedResponsesStreamItems(events []canonical.Event) []canonical.CanonicalItem {
	var items []canonical.CanonicalItem
	for _, event := range events {
		if event.Kind != canonical.EventItemCompleted {
			continue
		}
		itemEvent, ok := event.Payload.(canonical.ItemEvent)
		if !ok {
			continue
		}
		completed, ok := itemEvent.Payload.(canonical.ItemCompletedPayload)
		if ok {
			items = append(items, completed.Item)
		}
	}
	return items
}

func TestDecodeResponseStreamRejectsMessageCheckpointForDifferentOpenIndexBeforeCompletion(t *testing.T) {
	raw := responsesCreatedFrame() +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_0\",\"output_index\":0,\"content_index\":0,\"delta\":\"hel\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"message\",\"id\":\"msg_1\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n\n" +
		responsesCompletedFrame("[]", "")
	stream := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	completed := 0
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			if !strings.Contains(err.Error(), "backend error from responses") {
				t.Fatalf("stream error = %v, want backend-origin mismatch", err)
			}
			break
		}
		if event.Kind == canonical.EventItemCompleted {
			completed++
		}
	}
	if completed != 0 {
		t.Fatalf("completed items before mismatched-index error = %d, want 0", completed)
	}
}

func TestDecodeResponseStreamReconcilesReasoningCheckpointsByKindAndIndex(t *testing.T) {
	tests := []struct {
		name          string
		eventType     string
		indexField    string
		terminalField string
		wantKind      canonical.ReasoningPartKind
	}{
		{name: "summary", eventType: "response.reasoning_summary_text.delta", indexField: "summary_index", terminalField: `"summary":[{"type":"summary_text","text":"hello"}]`, wantKind: canonical.ReasoningPartSummary},
		{name: "trace", eventType: "response.reasoning_text.delta", indexField: "content_index", terminalField: `"content":[{"type":"reasoning_text","text":"hello"}]`, wantKind: canonical.ReasoningPartTrace},
	}
	for _, test := range tests {
		for _, terminalOnly := range []bool{false, true} {
			path := "output_item_done"
			if terminalOnly {
				path = "response_completed"
			}
			t.Run(test.name+"/"+path, func(t *testing.T) {
				item := `{"type":"reasoning","id":"rs_1","status":"completed",` + test.terminalField + `}`
				middle := "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + item + "}\n\n"
				output := "[]"
				if terminalOnly {
					middle = ""
					output = "[" + item + "]"
				}
				raw := responsesCreatedFrame() +
					"event: " + test.eventType + "\ndata: {\"type\":\"" + test.eventType + "\",\"output_index\":0,\"item_id\":\"rs_1\",\"" + test.indexField + "\":0,\"delta\":\"hel\"}\n\n" +
					middle +
					responsesCompletedFrame(output, "")
				response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, raw)
				reasoning, ok := response.Items()[0].Reasoning()
				if !ok || len(reasoning.Parts()) != 1 {
					t.Fatalf("reasoning output = %#v, want one part", response.Items())
				}
				part := reasoning.Parts()[0]
				if part.Kind() != test.wantKind || part.Text() != "hello" {
					t.Fatalf("reasoning part = %#v, want %s hello", part, test.wantKind)
				}
			})
		}
	}
}

func TestDecodeResponseStreamRejectsContradictoryReasoningCheckpoints(t *testing.T) {
	for _, terminalOnly := range []bool{false, true} {
		name := "output_item_done"
		if terminalOnly {
			name = "response_completed"
		}
		t.Run(name, func(t *testing.T) {
			item := `{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"world"}]}`
			middle := "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + item + "}\n\n"
			output := "[]"
			if terminalOnly {
				middle = ""
				output = "[" + item + "]"
			}
			raw := responsesCreatedFrame() +
				"event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"item_id\":\"rs_1\",\"summary_index\":0,\"delta\":\"hel\"}\n\n" +
				middle +
				responsesCompletedFrame(output, "")
			assertResponsesStreamFails(t, canonical.CanonicalRequest{}, raw)
		})
	}
}

func TestDecodeResponseStreamRejectsMissingOrChangedReasoningCheckpoint(t *testing.T) {
	tests := map[string]string{
		"missing":     `{"type":"reasoning","id":"rs_1","status":"completed","summary":[]}`,
		"kind change": `{"type":"reasoning","id":"rs_1","status":"completed","content":[{"type":"reasoning_text","text":"hello"}]}`,
	}
	for name, item := range tests {
		t.Run(name, func(t *testing.T) {
			raw := responsesCreatedFrame() +
				"event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"item_id\":\"rs_1\",\"summary_index\":0,\"delta\":\"hel\"}\n\n" +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + item + "}\n\n"
			assertResponsesStreamFails(t, canonical.CanonicalRequest{}, raw)
		})
	}
}

func TestDecodeResponseStreamRejectsReasoningDeltaWithWrongIndexSpace(t *testing.T) {
	tests := map[string]string{
		"summary with content index": "event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"output_index\":0,\"item_id\":\"rs_1\",\"content_index\":0,\"delta\":\"hel\"}\n\n",
		"trace with summary index":   "event: response.reasoning_text.delta\ndata: {\"type\":\"response.reasoning_text.delta\",\"output_index\":0,\"item_id\":\"rs_1\",\"summary_index\":0,\"delta\":\"hel\"}\n\n",
	}
	for name, frame := range tests {
		t.Run(name, func(t *testing.T) {
			assertResponsesStreamFails(t, canonical.CanonicalRequest{}, responsesCreatedFrame()+frame)
		})
	}
}

func responsesFunctionRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	tool := canonicaltest.MustFunctionTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup"),
		"",
		canonicaltest.Schema(t, `{"type":"object"}`),
		canonical.Unspecified[bool](),
	)
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tool)}})
}

func responsesCustomRequest(t *testing.T) canonical.CanonicalRequest {
	t.Helper()
	tool := canonicaltest.MustCustomTool(
		canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "apply_patch"),
		"",
		canonical.NewToolFormatObject(canonicaltest.Object(t, `{"type":"grammar"}`)),
	)
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tool)}})
}

func responsesCreatedFrame() string {
	return "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n"
}

func responsesCompletedFrame(output string, outputText string) string {
	return "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":" + output + ",\"output_text\":\"" + outputText + "\"}}\n\n"
}

func readResponsesStreamResponse(t *testing.T, request canonical.CanonicalRequest, raw string) *canonical.CanonicalResponse {
	t.Helper()
	stream := decodeResponseStream(request, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "swobu_1"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertResponsesStreamFails(t *testing.T, request canonical.CanonicalRequest, raw string) {
	t.Helper()
	stream := decodeResponseStream(request, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "swobu_1"})
	if _, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse); err == nil {
		t.Fatal("Responses stream completed successfully")
	} else if !strings.Contains(err.Error(), "backend error from responses") {
		t.Fatalf("stream error = %v, want backend-origin lifecycle failure", err)
	}
}
