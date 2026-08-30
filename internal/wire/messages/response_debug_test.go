package messages

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestMessagesProjectionDebugLogsStructureWithoutContent(t *testing.T) {
	const canary = "SWOBU_PRIVATE_MESSAGE_CANARY"

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logMessagesStreamProjectionFrame([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"" + canary + "\"}}\n\n"))
	logMessagesStreamProjectionFrame([]byte("event: " + canary + "\nmalformed\n\n"))
	logMessagesStreamProjection(canonical.EventTextDelta)

	got := logs.String()
	if strings.Contains(got, canary) {
		t.Fatalf("Messages output content reached logs: %s", got)
	}
	for _, structural := range []string{
		"component=protocol.messages",
		"event=messages_stream_projection_frame",
		"frame_bytes=",
		"event=messages_stream_projection",
		"canonical_kind=text.delta",
	} {
		if !strings.Contains(got, structural) {
			t.Fatalf("logs missing structural field %q: %s", structural, got)
		}
	}
	if strings.Contains(got, "egress") {
		t.Fatalf("codec projection claimed client egress: %s", got)
	}
}
