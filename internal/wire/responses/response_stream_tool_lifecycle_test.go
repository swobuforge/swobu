package responses

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseStream_DoesNotReopenAnonymousToolCallOnSecondDoneFrame(t *testing.T) {
	t.Parallel()

	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":1,\"item\":{\"id\":\"call_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"Bash\"}}\n\n" +
		"event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":1,\"item_id\":\"call_1\",\"call_id\":\"call_1\",\"name\":\"Bash\",\"delta\":\"{\\\"command\\\":\\\"cat fixture\\\"}\"}\n\n" +
		"event: response.function_call_arguments.done\ndata: {\"type\":\"response.function_call_arguments.done\",\"output_index\":1,\"item_id\":\"call_1\",\"call_id\":\"call_1\",\"name\":\"Bash\",\"arguments\":\"{\\\"command\\\":\\\"cat fixture\\\"}\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"id\":\"call_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"arguments\":\"{\\\"command\\\":\\\"cat fixture\\\"}\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"

	reader := decodeResponseStream(
		carrier.WireStream{Frames: carrier.FrameReaderFromReadCloser(io.NopCloser(strings.NewReader(raw)))},
		"ex_stream_tool_lifecycle",
		nil,
	)

	closed, err := canonical.ReadClosedEnvelope(context.Background(), reader, canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	out, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	items := out.Items()
	if len(items) != 1 {
		t.Fatalf("output items len=%d, want 1", len(items))
	}
	item := items[0]
	if item.Kind != canonical.ItemKindToolUse {
		t.Fatalf("output item kind=%s, want %s", item.Kind, canonical.ItemKindToolUse)
	}
	if item.Name != "Bash" {
		t.Fatalf("output item name=%q, want Bash", item.Name)
	}
	if item.ToolUseID != "call_1" {
		t.Fatalf("tool use id=%q, want call_1", item.ToolUseID)
	}
}
