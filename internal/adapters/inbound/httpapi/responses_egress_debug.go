package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
)

const responsesDebugJSONMaxBytes = 4096

func logResponsesEgressBuffered(payload []byte) {
	normalized, truncated := compactAndTruncateJSON(payload, responsesDebugJSONMaxBytes)
	slog.Debug("responses buffered egress",
		"component", "httpapi",
		"event", "responses_buffered_egress",
		"payload_truncated", truncated,
		"payload_json", normalized,
	)
}

func logResponsesEgressStreamFrame(raw []byte) {
	var frame map[string]any
	if err := json.Unmarshal(raw, &frame); err != nil {
		slog.Debug("responses stream egress frame",
			"component", "httpapi",
			"event", "responses_stream_egress_frame",
			"frame_type", "unknown",
			"decode_error", err.Error(),
		)
		return
	}

	frameType, _ := frame["type"].(string)
	normalized, truncated := compactAndTruncateJSON(raw, responsesDebugJSONMaxBytes)
	slog.Debug("responses stream egress frame",
		"component", "httpapi",
		"event", "responses_stream_egress_frame",
		"frame_type", strings.TrimSpace(frameType),
		"frame_truncated", truncated,
		"frame_json", normalized,
	)
}

func compactAndTruncateJSON(raw []byte, maxBytes int) (string, bool) {
	text := strings.TrimSpace(string(raw))
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
