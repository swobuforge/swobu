package responses

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestResponsesEgressDebugLogsStructureWithoutContent(t *testing.T) {
	const canary = "SWOBU_PRIVATE_RESPONSE_CANARY"

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logResponsesEgressBuffered([]byte(`{"output_text":"` + canary + `"}`))
	logResponsesEgressStreamFrame([]byte(`{"type":"response.output_text.delta","delta":"` + canary + `"}`))
	logResponsesEgressStreamFrame([]byte(`{"type":"` + canary))

	got := logs.String()
	if strings.Contains(got, canary) {
		t.Fatalf("Responses output content reached logs: %s", got)
	}
	for _, structural := range []string{
		"event=responses_buffered_egress",
		"payload_bytes=",
		"event=responses_stream_egress_frame",
		"frame_bytes=",
	} {
		if !strings.Contains(got, structural) {
			t.Fatalf("logs missing structural field %q: %s", structural, got)
		}
	}
}

func TestResponsesTerminalCheckpointMismatchLogsStructureWithoutContent(t *testing.T) {
	const checkpointCanary = "SWOBU_PRIVATE_CHECKPOINT_CANARY"
	const terminalCanary = "SWOBU_PRIVATE_TERMINAL_CANARY"

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"private_item_id\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"" + checkpointCanary + "\"}]}}\n\n" +
		responsesCompletedFrame(`[{"type":"message","id":"private_item_id","status":"completed","content":[{"type":"output_text","text":"`+terminalCanary+`"}]}]`, "")
	stream := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "exchange_123", nil, true)
	for {
		if _, err := stream.Next(context.Background()); err != nil {
			if !strings.Contains(err.Error(), "terminal output contradicts completed semantic checkpoint") {
				t.Fatalf("stream error = %v", err)
			}
			break
		}
	}

	got := logs.String()
	for _, field := range []string{
		"event=responses_terminal_checkpoint_mismatch",
		"exchange_id=exchange_123",
		"output_index=0",
		"checkpoint_item_count=1",
		"terminal_item_count=1",
		"checkpoint_kinds=message",
		"terminal_kinds=message",
		"mismatch=message_content",
	} {
		if !strings.Contains(got, field) {
			t.Fatalf("logs missing structural field %q: %s", field, got)
		}
	}
	for _, sensitive := range []string{checkpointCanary, terminalCanary, "private_item_id"} {
		if strings.Contains(got, sensitive) {
			t.Fatalf("Responses output content reached mismatch log: %s", got)
		}
	}
}

func TestResponsesMatchingTerminalCheckpointEmitsNoMismatchWarning(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	item := `{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"same"}]}`
	raw := responsesCreatedFrame() +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + item + "}\n\n" +
		responsesCompletedFrame("["+item+"]", "")
	response := readResponsesStreamResponse(t, canonical.CanonicalRequest{}, raw)
	if len(response.Items()) != 1 {
		t.Fatalf("response items = %d, want 1", len(response.Items()))
	}
	if strings.Contains(logs.String(), "responses_terminal_checkpoint_mismatch") {
		t.Fatalf("matching terminal checkpoint emitted mismatch warning: %s", logs.String())
	}
}
