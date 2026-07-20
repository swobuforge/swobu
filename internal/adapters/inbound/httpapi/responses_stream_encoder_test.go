package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
	responses "github.com/swobuforge/swobu/internal/wire/responses"
)

// Reference lifecycle: https://developers.openai.com/api/reference/resources/responses/methods/create
// response.created -> response.output_item.added -> response.content_part.added
// -> response.output_text.delta -> response.output_text.done -> response.content_part.done
// -> response.output_item.done -> response.completed
func TestResponsesWireEventEncoder_TextLifecycleMatchesOfficialOrder(t *testing.T) {
	encoder := responses.NewResponseStreamWireEncoder()
	events := []sse.StreamEvent{
		{Kind: sse.StreamEventStarted, ResultID: "resp_1", Model: "m"},
		{Kind: sse.StreamEventTextDelta, ItemID: "text_0", TextDelta: "ok"},
		{Kind: sse.StreamEventCompleted, FinishReason: "completed", Usage: mustUsageForStream(t, 12, 2, 6, 1)},
	}

	frames := encodeAllFrames(t, &encoder, events)
	if got, want := eventTypes(frames), []string{
		"response.created",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",
		"response.completed",
	}; !equalStrings(got, want) {
		t.Fatalf("event types = %#v, want %#v", got, want)
	}

	created := frames[0]
	if response := objectAt(created, "response"); response["status"] != "in_progress" {
		t.Fatalf("response.created status = %v, want in_progress", response["status"])
	}
	delta := frames[3]
	if _, ok := delta["item_id"].(string); !ok {
		t.Fatalf("response.output_text.delta missing item_id: %#v", delta)
	}
	if _, ok := delta["output_index"].(float64); !ok {
		t.Fatalf("response.output_text.delta missing output_index: %#v", delta)
	}
	if _, ok := delta["content_index"].(float64); !ok {
		t.Fatalf("response.output_text.delta missing content_index: %#v", delta)
	}
	completed := frames[len(frames)-1]
	response := objectAt(completed, "response")
	if response["status"] != "completed" {
		t.Fatalf("response.completed status = %v, want completed", response["status"])
	}
	output, ok := response["output"].([]any)
	if !ok || len(output) == 0 {
		t.Fatalf("response.completed output = %#v, want non-empty list", response["output"])
	}
	usage, ok := response["usage"].(map[string]any)
	if !ok {
		t.Fatalf("response.completed usage = %#v, want usage object", response["usage"])
	}
	if got := usage["input_tokens"]; got != float64(12) {
		t.Fatalf("usage.input_tokens = %v, want 12", got)
	}
	if got := usage["output_tokens"]; got != float64(2) {
		t.Fatalf("usage.output_tokens = %v, want 2", got)
	}
}

func TestResponsesWireEventEncoder_CompletedWithNoItemsFailsClosed(t *testing.T) {
	encoder := responses.NewResponseStreamWireEncoder()

	frames := encodeAllFrames(t, &encoder, []sse.StreamEvent{
		{Kind: sse.StreamEventStarted, ResultID: "resp_empty", Model: "m"},
		{Kind: sse.StreamEventCompleted, ResultID: "resp_empty", Model: "m"},
	})

	if completed := frameOfType(frames, "response.completed"); completed != nil {
		t.Fatalf("unexpected successful completion frame: %#v", completed)
	}
	failed := frameOfType(frames, "response.failed")
	if failed == nil {
		t.Fatal("missing response.failed frame")
	}
	response := objectAt(failed, "response")
	if got := response["status"]; got != "failed" {
		t.Fatalf("response.failed status = %#v, want failed", got)
	}
	output, ok := response["output"].([]any)
	if !ok {
		t.Fatalf("response.failed output = %#v, want empty array", response["output"])
	}
	if len(output) != 0 {
		t.Fatalf("response.failed output len = %d, want 0", len(output))
	}
	errObj := objectAt(response, "error")
	if got := errObj["code"]; got != "stream_completed_without_output_items" {
		t.Fatalf("response.failed error code = %#v, want stream_completed_without_output_items", got)
	}
}

func TestResponsesWireEventEncoder_FailedDoesNotLookLikeSuccessfulCompletion(t *testing.T) {
	encoder := responses.NewResponseStreamWireEncoder()

	frames := encodeAllFrames(t, &encoder, []sse.StreamEvent{
		{Kind: sse.StreamEventStarted, ResultID: "resp_failed", Model: "m"},
		{
			Kind:         sse.StreamEventFailed,
			ResultID:     "resp_failed",
			Model:        "m",
			ErrorCode:    "stream_unexpected_eof",
			ErrorMessage: "output stream ended before completed",
		},
	})

	if completed := frameOfType(frames, "response.completed"); completed != nil {
		t.Fatalf("unexpected successful completion frame: %#v", completed)
	}
	failed := frameOfType(frames, "response.failed")
	if failed == nil {
		t.Fatal("missing response.failed frame")
	}
	response := objectAt(failed, "response")
	if got := response["status"]; got != "failed" {
		t.Fatalf("response.failed status = %#v, want failed", got)
	}
	output, ok := response["output"].([]any)
	if !ok || len(output) != 0 {
		t.Fatalf("response.failed output = %#v, want empty array", response["output"])
	}
	errObj := objectAt(response, "error")
	if got := errObj["code"]; got != "stream_unexpected_eof" {
		t.Fatalf("response.failed error code = %#v", got)
	}
	if got := errObj["message"]; got != "output stream ended before completed" {
		t.Fatalf("response.failed error message = %#v", got)
	}
}

func TestResponsesWireEventEncoder_ToolLifecycleIncludesItemFrames(t *testing.T) {
	encoder := responses.NewResponseStreamWireEncoder()
	events := []sse.StreamEvent{
		{Kind: sse.StreamEventStarted, ResultID: "resp_2", Model: "m"},
		{Kind: sse.StreamEventItemStarted, ItemKind: canonical.ItemKindToolCall, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep"},
		{Kind: sse.StreamEventToolUseArgumentsDelta, ItemKind: canonical.ItemKindToolCall, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep", ArgumentsDelta: "{\"pattern\":\"TODO\"}"},
		{Kind: sse.StreamEventItemCompleted, ItemKind: canonical.ItemKindToolCall, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep"},
		{Kind: sse.StreamEventCompleted, FinishReason: "completed"},
	}
	frames := encodeAllFrames(t, &encoder, events)
	types := eventTypes(frames)
	if !contains(types, "response.output_item.added") {
		t.Fatalf("event types = %#v, want response.output_item.added", types)
	}
	if !contains(types, "response.function_call_arguments.delta") {
		t.Fatalf("event types = %#v, want response.function_call_arguments.delta", types)
	}
	if !contains(types, "response.output_item.done") {
		t.Fatalf("event types = %#v, want response.output_item.done", types)
	}
	doneFrame := frameOfType(frames, "response.function_call_arguments.done")
	if doneFrame == nil {
		t.Fatal("missing response.function_call_arguments.done frame")
	}
	if got, ok := doneFrame["arguments"].(string); !ok || got != "{\"pattern\":\"TODO\"}" {
		t.Fatalf("response.function_call_arguments.done arguments = %#v, want pattern JSON", doneFrame["arguments"])
	}
	if _, ok := doneFrame["input"]; ok {
		t.Fatalf("response.function_call_arguments.done unexpectedly included input: %#v", doneFrame)
	}
	completed := frames[len(frames)-1]
	response := objectAt(completed, "response")
	output, ok := response["output"].([]any)
	if !ok || len(output) == 0 {
		t.Fatalf("response.completed output = %#v, want non-empty list", response["output"])
	}
	toolOutput, ok := output[0].(map[string]any)
	if !ok {
		t.Fatalf("response.completed output[0] = %#v, want object", output[0])
	}
	if got, ok := toolOutput["arguments"].(string); !ok || got != "{\"pattern\":\"TODO\"}" {
		t.Fatalf("response.completed output[0].arguments = %#v, want pattern JSON", toolOutput["arguments"])
	}
	if _, ok := toolOutput["input"]; ok {
		t.Fatalf("response.completed output[0] unexpectedly included input: %#v", toolOutput)
	}
	for _, frame := range frames {
		if frame["type"] == "response.function_call_arguments.delta" || frame["type"] == "response.function_call_arguments.done" {
			if _, ok := frame["output_index"].(float64); !ok {
				t.Fatalf("%s missing output_index: %#v", frame["type"], frame)
			}
		}
	}
}

func TestResponsesWireEventEncoder_CustomToolLifecycleUsesInputField(t *testing.T) {
	encoder := responses.NewResponseStreamWireEncoder()
	events := []sse.StreamEvent{
		{Kind: sse.StreamEventStarted, ResultID: "resp_3", Model: "m"},
		{Kind: sse.StreamEventItemStarted, ItemKind: canonical.ItemKindToolCall, ItemID: "custom_0", ToolUseID: "call_2", Name: "apply_patch", ToolType: canonical.ToolTypeCustom},
		{Kind: sse.StreamEventToolUseArgumentsDelta, ItemKind: canonical.ItemKindToolCall, ItemID: "custom_0", ToolUseID: "call_2", Name: "apply_patch", ToolType: canonical.ToolTypeCustom, ArgumentsDelta: "{\"patch\":\"x\"}"},
		{Kind: sse.StreamEventItemCompleted, ItemKind: canonical.ItemKindToolCall, ItemID: "custom_0", ToolUseID: "call_2", Name: "apply_patch", ToolType: canonical.ToolTypeCustom},
		{Kind: sse.StreamEventCompleted, FinishReason: "completed"},
	}
	frames := encodeAllFrames(t, &encoder, events)

	doneFrame := frameOfType(frames, "response.custom_tool_call_input.done")
	if doneFrame == nil {
		t.Fatal("missing response.custom_tool_call_input.done frame")
	}
	if got, ok := doneFrame["input"].(string); !ok || got != "{\"patch\":\"x\"}" {
		t.Fatalf("response.custom_tool_call_input.done input = %#v, want patch JSON", doneFrame["input"])
	}
	if _, ok := doneFrame["arguments"]; ok {
		t.Fatalf("response.custom_tool_call_input.done unexpectedly included arguments: %#v", doneFrame)
	}

	completed := frames[len(frames)-1]
	response := objectAt(completed, "response")
	output, ok := response["output"].([]any)
	if !ok || len(output) == 0 {
		t.Fatalf("response.completed output = %#v, want non-empty list", response["output"])
	}
	toolOutput, ok := output[0].(map[string]any)
	if !ok {
		t.Fatalf("response.completed output[0] = %#v, want object", output[0])
	}
	if got, ok := toolOutput["input"].(string); !ok || got != "{\"patch\":\"x\"}" {
		t.Fatalf("response.completed output[0].input = %#v, want patch JSON", toolOutput["input"])
	}
	if _, ok := toolOutput["arguments"]; ok {
		t.Fatalf("response.completed output[0] unexpectedly included arguments: %#v", toolOutput)
	}
}

func TestResponsesWireEventEncoder_CompletedUsageIncludesCachedTokensWhenZeroButPresent(t *testing.T) {
	encoder := responses.NewResponseStreamWireEncoder()
	input, output := 5, 2
	cacheRead, cacheWrite := 0, 0
	usage, err := canonical.NewTokenUsageWithOptional(&input, &output, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}

	frames := encodeAllFrames(t, &encoder, []sse.StreamEvent{
		{Kind: sse.StreamEventStarted, ResultID: "resp_usage_1", Model: "m"},
		{Kind: sse.StreamEventTextDelta, ItemID: "text_0", TextDelta: "ok"},
		{Kind: sse.StreamEventCompleted, Usage: usage},
	})

	completed := frames[len(frames)-1]
	response := objectAt(completed, "response")
	usageDTO, ok := response["usage"].(map[string]any)
	if !ok {
		t.Fatalf("response.usage = %#v, want object", response["usage"])
	}
	inputDetails, ok := usageDTO["input_tokens_details"].(map[string]any)
	if !ok {
		t.Fatalf("usage.input_tokens_details = %#v, want object", usageDTO["input_tokens_details"])
	}
	if got := inputDetails["cached_tokens"]; got != float64(0) {
		t.Fatalf("usage.input_tokens_details.cached_tokens = %#v, want 0", got)
	}
}

func encodeAllFrames(t *testing.T, encoder *responses.ResponseStreamWireEncoder, events []sse.StreamEvent) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		frames, err := encoder.Encode(sse.StreamEvent{
			Kind:           event.Kind,
			ResultID:       event.ResultID,
			Model:          event.Model,
			ItemKind:       event.ItemKind,
			ItemID:         event.ItemID,
			TextDelta:      event.TextDelta,
			ToolUseID:      event.ToolUseID,
			Name:           event.Name,
			ToolType:       event.ToolType,
			ArgumentsDelta: event.ArgumentsDelta,
			FinishReason:   event.FinishReason,
			Usage:          event.Usage,
			ErrorCode:      event.ErrorCode,
			ErrorMessage:   event.ErrorMessage,
		})
		if err != nil {
			t.Fatalf("Encode(%s) returned error: %v", event.Kind, err)
		}
		for _, frame := range frames {
			out = append(out, decodeFrame(t, frame))
		}
	}
	tail, err := encoder.Finish()
	if err != nil {
		t.Fatalf("Finish returned error: %v", err)
	}
	for _, frame := range tail {
		out = append(out, decodeFrame(t, frame))
	}
	return out
}

func decodeFrame(t *testing.T, frame []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(frame, &out); err != nil {
		t.Fatalf("frame JSON decode failed: %v frame=%s", err, string(frame))
	}
	return out
}

func eventTypes(frames []map[string]any) []string {
	out := make([]string, 0, len(frames))
	for _, frame := range frames {
		typ, _ := frame["type"].(string)
		out = append(out, typ)
	}
	return out
}

func objectAt(frame map[string]any, key string) map[string]any {
	raw, _ := frame[key].(map[string]any)
	return raw
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func frameOfType(frames []map[string]any, target string) map[string]any {
	for _, frame := range frames {
		if frame["type"] == target {
			return frame
		}
	}
	return nil
}

func equalStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func mustUsageForStream(t *testing.T, input, output, cacheRead, cacheWrite int) canonical.TokenUsage {
	t.Helper()
	usage, err := canonical.NewTokenUsageWithOptional(&input, &output, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}
	return usage
}
