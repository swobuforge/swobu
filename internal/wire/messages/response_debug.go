package messages

import "log/slog"

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
