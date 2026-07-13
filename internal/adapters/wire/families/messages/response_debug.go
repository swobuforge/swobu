package messages

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
)

const messagesDebugJSONMaxBytes = 4096

func logMessagesEgressBuffered(payload []byte) {
	normalized, truncated := compactAndTruncateJSON(payload, messagesDebugJSONMaxBytes)
	slog.Debug("messages buffered egress",
		"component", "httpapi",
		"event", "messages_buffered_egress",
		"payload_truncated", truncated,
		"payload_json", normalized,
	)
}

func logMessagesEgressStreamFrame(raw []byte) {
	frameType, payload, err := decodeSSEFramePayload(raw)
	if err != nil {
		slog.Debug("messages stream egress frame",
			"component", "httpapi",
			"event", "messages_stream_egress_frame",
			"frame_type", "unknown",
			"decode_error", err.Error(),
			"frame_raw", compactAndTruncateString(string(raw), messagesDebugJSONMaxBytes),
		)
		return
	}

	normalized, truncated := compactAndTruncateJSON(payload, messagesDebugJSONMaxBytes)
	slog.Debug("messages stream egress frame",
		"component", "httpapi",
		"event", "messages_stream_egress_frame",
		"frame_type", strings.TrimSpace(frameType), // swobu:io-string source=boundary
		"frame_truncated", truncated,
		"frame_json", normalized,
	)
}

func decodeSSEFramePayload(raw []byte) (string, []byte, error) {
	lines := strings.Split(string(raw), "\n")
	eventName := ""
	data := ""
	for _, line := range lines {
		line = strings.TrimSpace(line) // swobu:io-string source=boundary
		switch {
		case strings.HasPrefix(line, "event:"):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:")) // swobu:io-string source=boundary
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:")) // swobu:io-string source=boundary
		}
	}
	if strings.TrimSpace(data) == "" {
		return eventName, nil, errors.New("messages stream frame is missing data")
	}
	return eventName, []byte(data), nil
}

func compactAndTruncateJSON(raw []byte, maxBytes int) (string, bool) {
	text := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if text == "" {
		return "null", false
	}
	normalized := text
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(text)); err == nil {
		normalized = compact.String()
	}
	if maxBytes <= 0 || len(normalized) <= maxBytes {
		return normalized, false
	}
	return normalized[:maxBytes], true
}

func compactAndTruncateString(text string, maxBytes int) string {
	normalized := strings.TrimSpace(text) // swobu:io-string source=boundary
	if maxBytes <= 0 || len(normalized) <= maxBytes {
		return normalized
	}
	return normalized[:maxBytes]
}
