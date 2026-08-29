package messages

import (
	"log/slog"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func logMessagesEgressBuffered(payload []byte) {
	slog.Debug("messages buffered egress",
		"component", "httpapi",
		"event", "messages_buffered_egress",
		"payload_bytes", len(payload),
	)
}

func logMessagesEgressStreamFrame(raw []byte) {
	slog.Debug("messages stream egress frame",
		"component", "httpapi",
		"event", "messages_stream_egress_frame",
		"frame_bytes", len(raw),
	)
}

func logMessagesStreamProjection(kind canonical.EventKind) {
	slog.Debug("messages stream projection",
		"component", "httpapi",
		"event", "messages_stream_projection",
		"canonical_kind", string(kind),
	)
}
