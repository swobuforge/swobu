package responses

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
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
		"frame_type", strings.TrimSpace(frameType), // swobu:io-string source=boundary
		"frame_truncated", truncated,
		"frame_json", normalized,
	)
}

func logResponsesTerminalProjection(usedFallback bool, status string, rawOutputCount int, rawOutputTextPresent bool, items []canonical.OutputItem) {
	fallbackTextCount := 0
	fallbackToolUseCount := 0
	textPreview := ""
	for _, item := range items {
		switch item.Kind() {
		case canonical.ItemKindText:
			fallbackTextCount++
			if textPreview == "" {
				if text, ok := item.TextItem(); ok {
					textPreview = strings.TrimSpace(text.Text) // swobu:io-string source=log-formatting
				}
			}
		case canonical.ItemKindToolUse:
			fallbackToolUseCount++
		}
	}
	slog.Debug("responses terminal projection",
		"component", "httpapi",
		"event", "responses_terminal_projection",
		"used_fallback", usedFallback,
		"status", strings.TrimSpace(status), // swobu:io-string source=log-formatting
		"raw_output_count", rawOutputCount,
		"raw_output_text_present", rawOutputTextPresent,
		"fallback_item_count", len(items),
		"fallback_text_count", fallbackTextCount,
		"fallback_tool_use_count", fallbackToolUseCount,
		"fallback_text_preview", textPreview,
	)
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
