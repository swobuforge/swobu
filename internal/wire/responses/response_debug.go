package responses

import (
	"log/slog"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func logResponsesStreamProjectionFrame(raw []byte) {
	slog.Debug("responses stream projection frame",
		"component", "protocol.responses",
		"event", "responses_stream_projection_frame",
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
		"component", "protocol.responses",
		"event", "responses_terminal_projection",
		"used_fallback", usedFallback,
		"raw_output_count", rawOutputCount,
		"raw_output_text_present", rawOutputTextPresent,
		"fallback_item_count", len(items),
		"fallback_text_count", fallbackTextCount,
		"fallback_tool_use_count", fallbackToolUseCount,
	)
}
