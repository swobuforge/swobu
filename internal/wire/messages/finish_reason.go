package messages

import "strings"

func messagesStopReasonForFinishReason(finishReason string, sawToolUse bool) string {
	normalized := strings.ToLower(strings.TrimSpace(finishReason)) // swobu:io-string source=boundary
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
		return "end_turn"
	}
}
