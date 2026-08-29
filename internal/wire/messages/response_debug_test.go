package messages

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestMessagesEgressDebugLogsStructureWithoutContent(t *testing.T) {
	const canary = "SWOBU_PRIVATE_MESSAGE_CANARY"

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logMessagesEgressBuffered([]byte(`{"content":[{"type":"text","text":"` + canary + `"}]}`))
	logMessagesEgressStreamFrame([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"" + canary + "\"}}\n\n"))
	logMessagesEgressStreamFrame([]byte("event: " + canary + "\nmalformed\n\n"))
	logMessagesStreamProjection(canonical.EventTextDelta)

	got := logs.String()
	if strings.Contains(got, canary) {
		t.Fatalf("Messages output content reached logs: %s", got)
	}
	for _, structural := range []string{
		"event=messages_buffered_egress",
		"payload_bytes=",
		"event=messages_stream_egress_frame",
		"frame_bytes=",
		"event=messages_stream_projection",
		"canonical_kind=text.delta",
	} {
		if !strings.Contains(got, structural) {
			t.Fatalf("logs missing structural field %q: %s", structural, got)
		}
	}
}
