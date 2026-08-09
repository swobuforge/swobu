package responses

import (
	"log/slog"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func logResponsesTerminalCheckpointMismatch(exchangeID string, outputIndex int, checkpointItems []canonical.CanonicalItem, terminalItems []canonical.CanonicalItem) {
	slog.Warn("responses terminal checkpoint mismatch",
		"component", "httpapi",
		"event", "responses_terminal_checkpoint_mismatch",
		"exchange_id", exchangeID,
		"output_index", outputIndex,
		"checkpoint_item_count", len(checkpointItems),
		"terminal_item_count", len(terminalItems),
		"checkpoint_kinds", responsesCanonicalKindSequence(checkpointItems),
		"terminal_kinds", responsesCanonicalKindSequence(terminalItems),
		"mismatch", responsesCanonicalMismatchCategory(checkpointItems, terminalItems),
	)
}

func responsesCanonicalKindSequence(items []canonical.CanonicalItem) string {
	kinds := make([]string, len(items))
	for index, item := range items {
		kinds[index] = string(item.Kind())
	}
	return strings.Join(kinds, ",")
}

// responsesCanonicalMismatchCategory deliberately reports only closed
// structural categories. Canonical values can contain model text, tool input,
// URLs, and provider correlation identifiers, none of which belong in logs.
func responsesCanonicalMismatchCategory(checkpointItems []canonical.CanonicalItem, terminalItems []canonical.CanonicalItem) string {
	if len(checkpointItems) != len(terminalItems) {
		return "item_count"
	}
	for index := range checkpointItems {
		if checkpointItems[index].Kind() != terminalItems[index].Kind() {
			return "item_kind"
		}
		if responsesCanonicalItemsEqual(checkpointItems[index:index+1], terminalItems[index:index+1]) {
			continue
		}
		switch checkpointItems[index].Kind() {
		case canonical.ItemKindMessage:
			return "message_content"
		case canonical.ItemKindToolDeclarations:
			return "tool_declarations"
		case canonical.ItemKindToolCall:
			return "tool_call"
		case canonical.ItemKindToolResult:
			return "tool_result"
		case canonical.ItemKindToolDiscoveryResult:
			return "tool_discovery_result"
		case canonical.ItemKindReasoning:
			return responsesReasoningMismatchCategory(checkpointItems[index], terminalItems[index])
		default:
			return "canonical_item"
		}
	}
	return "canonical_item"
}

func responsesReasoningMismatchCategory(checkpoint canonical.CanonicalItem, terminal canonical.CanonicalItem) string {
	checkpointReasoning, checkpointOK := checkpoint.Reasoning()
	terminalReasoning, terminalOK := terminal.Reasoning()
	if !checkpointOK || !terminalOK {
		return "reasoning_shape"
	}
	checkpointParts := checkpointReasoning.Parts()
	terminalParts := terminalReasoning.Parts()
	if len(checkpointParts) != len(terminalParts) {
		return "reasoning_part_count"
	}
	for index := range checkpointParts {
		if checkpointParts[index].Kind() != terminalParts[index].Kind() {
			return "reasoning_part_kind"
		}
		if checkpointParts[index].Text() != terminalParts[index].Text() {
			return "reasoning_part_content"
		}
	}
	checkpointReplay, checkpointReplayOK := checkpointReasoning.Opaque().Responses()
	terminalReplay, terminalReplayOK := terminalReasoning.Opaque().Responses()
	if checkpointReplayOK != terminalReplayOK {
		return "reasoning_replay_presence"
	}
	if checkpointReplayOK {
		if checkpointReplay.EncryptedContent != terminalReplay.EncryptedContent {
			return "reasoning_replay_content"
		}
		checkpointIDPresent := checkpointReplay.ItemID != ""
		terminalIDPresent := terminalReplay.ItemID != ""
		if checkpointIDPresent != terminalIDPresent {
			return "reasoning_replay_id_presence"
		}
		if checkpointReplay.ItemID != terminalReplay.ItemID {
			return "reasoning_replay_id"
		}
	}
	return "reasoning"
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
