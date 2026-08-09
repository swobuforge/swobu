package responses

import (
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func logResponsesTerminalCheckpointMismatch(exchangeID string, outputIndex int, checkpointItems []canonical.CanonicalItem, terminalItems []canonical.CanonicalItem, mismatch string) {
	slog.Warn("responses terminal checkpoint mismatch",
		"component", "httpapi",
		"event", "responses_terminal_checkpoint_mismatch",
		"exchange_id", exchangeID,
		"output_index", outputIndex,
		"checkpoint_item_count", len(checkpointItems),
		"terminal_item_count", len(terminalItems),
		"checkpoint_kinds", responsesCanonicalKindSequence(checkpointItems),
		"terminal_kinds", responsesCanonicalKindSequence(terminalItems),
		"mismatch", mismatch,
	)
}

func responsesCanonicalKindSequence(items []canonical.CanonicalItem) string {
	kinds := make([]string, len(items))
	for index, item := range items {
		kinds[index] = string(item.Kind())
	}
	return strings.Join(kinds, ",")
}

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
