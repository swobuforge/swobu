package messages

import (
	"log/slog"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func logMessagesStreamProjectionFrame(raw []byte) {
	slog.Debug("messages stream projection frame",
		"component", "protocol.messages",
		"event", "messages_stream_projection_frame",
		"frame_bytes", len(raw),
	)
}

func logMessagesStreamProjection(kind canonical.EventKind) {
	slog.Debug("messages stream projection",
		"component", "protocol.messages",
		"event", "messages_stream_projection",
		"canonical_kind", string(kind),
	)
}
