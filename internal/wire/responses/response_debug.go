package responses

import (
	"log/slog"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func logResponsesEgressBuffered(payload []byte) {
	slog.Debug("responses buffered egress",
		"component", "httpapi",
		"event", "responses_buffered_egress",
		"payload_bytes", len(payload),
	)
}

func logResponsesEgressStreamFrame(raw []byte) {
	slog.Debug("responses stream egress frame",
		"component", "httpapi",
		"event", "responses_stream_egress_frame",
		"frame_bytes", len(raw),
	)
}

func logResponsesTerminalProjection(usedFallback bool, rawOutputCount int, rawOutputTextPresent bool, items []canonical.CanonicalItem) {
	fallbackTextCount := 0
	fallbackToolUseCount := 0
	for _, item := range items {
		switch item.Kind() {
		case canonical.ItemKindMessage:
			fallbackTextCount++
		case canonical.ItemKindToolCall:
			fallbackToolUseCount++
		}
	}
	slog.Debug("responses terminal projection",
		"component", "httpapi",
		"event", "responses_terminal_projection",
		"used_fallback", usedFallback,
		"raw_output_count", rawOutputCount,
		"raw_output_text_present", rawOutputTextPresent,
		"fallback_item_count", len(items),
		"fallback_text_count", fallbackTextCount,
		"fallback_tool_use_count", fallbackToolUseCount,
	)
}
