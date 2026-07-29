package messages

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func messagesCompletion(reason string) canonical.Completion {
	switch strings.ToLower(strings.TrimSpace(reason)) { // swobu:io-string source=provider-wire
	case "max_tokens", "max_output_tokens", "length", "pause_turn":
		return canonical.Incomplete(reason)
	case "refusal", "content_filter", "content_filtered", "safety":
		return canonical.Declined(reason)
	case "end_turn", "stop_sequence", "tool_use", "stop", "completed":
		return canonical.Completed(reason)
	default:
		return canonical.Failed(reason)
	}
}

func messagesStopReasonForCompletion(completion canonical.Completion, sawToolUse bool) string {
	finishReason := strings.TrimSpace(completion.Reason()) // swobu:io-string source=boundary
	normalized := strings.ToLower(finishReason)
	switch normalized {
	case "tool_use", "tool_calls":
		return "tool_use"
	case "refusal", "pause_turn":
		return normalized
	case "max_output_tokens", "length", "max_tokens":
		return "max_tokens"
	case "stop", "end_turn", "completed":
		if sawToolUse {
			return "tool_use"
		}
		return "end_turn"
	default:
		if sawToolUse {
			return "tool_use"
		}
		if finishReason != "" {
			return finishReason
		}
		switch completion.Class() {
		case canonical.CompletionIncomplete:
			return "max_tokens"
		case canonical.CompletionDeclined:
			return "refusal"
		default:
			return "end_turn"
		}
	}
}
