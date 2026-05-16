package httpapi

import (
	"encoding/json"
	"testing"

	httpcodec "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/httpcodec"
	responses "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/responses"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// Reference lifecycle: https://developers.openai.com/api/reference/resources/responses/methods/create
// response.created -> response.output_item.added -> response.content_part.added
// -> response.output_text.delta -> response.output_text.done -> response.content_part.done
// -> response.output_item.done -> response.completed
func TestResponsesWireEventEncoder_TextLifecycleMatchesOfficialOrder(t *testing.T) {
	encoder := responses.NewResponsesClientStreamEncoderWire()
	events := []httpcodec.StreamEvent{
		{Kind: httpcodec.StreamEventStarted, ResultID: "resp_1", Model: "m"},
		{Kind: httpcodec.StreamEventTextDelta, ItemID: "text_0", TextDelta: "ok"},
		{Kind: httpcodec.StreamEventCompleted, FinishReason: "completed", Usage: mustUsageForStream(t, 12, 2, 6, 1)},
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

func TestResponsesWireEventEncoder_ToolLifecycleIncludesItemFrames(t *testing.T) {
	encoder := responses.NewResponsesClientStreamEncoderWire()
	events := []httpcodec.StreamEvent{
		{Kind: httpcodec.StreamEventStarted, ResultID: "resp_2", Model: "m"},
		{Kind: httpcodec.StreamEventItemStarted, ItemKind: canonical.ItemKindToolUse, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep"},
		{Kind: httpcodec.StreamEventToolUseArgumentsDelta, ItemKind: canonical.ItemKindToolUse, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep", ArgumentsDelta: "{\"pattern\":\"TODO\"}"},
		{Kind: httpcodec.StreamEventItemCompleted, ItemKind: canonical.ItemKindToolUse, ItemID: "tool_0", ToolUseID: "call_1", Name: "grep"},
		{Kind: httpcodec.StreamEventCompleted, FinishReason: "completed"},
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
	for _, frame := range frames {
		if frame["type"] == "response.function_call_arguments.delta" || frame["type"] == "response.function_call_arguments.done" {
			if _, ok := frame["output_index"].(float64); !ok {
				t.Fatalf("%s missing output_index: %#v", frame["type"], frame)
			}
		}
	}
}

func TestResponsesWireEventEncoder_CompletedUsageIncludesCachedTokensWhenZeroButPresent(t *testing.T) {
	encoder := responses.NewResponsesClientStreamEncoderWire()
	input, output := 5, 2
	cacheRead, cacheWrite := 0, 0
	usage, err := canonical.NewTokenUsageWithOptional(&input, &output, &cacheRead, &cacheWrite)
	if err != nil {
		t.Fatalf("NewTokenUsageWithOptional returned error: %v", err)
	}

	frames := encodeAllFrames(t, &encoder, []httpcodec.StreamEvent{
		{Kind: httpcodec.StreamEventStarted, ResultID: "resp_usage_1", Model: "m"},
		{Kind: httpcodec.StreamEventTextDelta, ItemID: "text_0", TextDelta: "ok"},
		{Kind: httpcodec.StreamEventCompleted, Usage: usage},
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

func encodeAllFrames(t *testing.T, encoder *responses.ResponsesClientStreamEncoderWire, events []httpcodec.StreamEvent) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		frames, err := encoder.Encode(httpcodec.StreamEvent{
			Kind:           event.Kind,
			ResultID:       event.ResultID,
			Model:          event.Model,
			ItemKind:       event.ItemKind,
			ItemID:         event.ItemID,
			TextDelta:      event.TextDelta,
			ToolUseID:      event.ToolUseID,
			Name:           event.Name,
			ArgumentsDelta: event.ArgumentsDelta,
			FinishReason:   event.FinishReason,
			Usage:          event.Usage,
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
