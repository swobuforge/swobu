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

func messagesStopReasonForCompletion(completion canonical.Completion, sawToolUse bool) (string, error) {
	switch completion.Class() {
	case canonical.CompletionCompleted:
		if sawToolUse {
			return "tool_use", nil
		}
		return "end_turn", nil
	case canonical.CompletionIncomplete:
		// pause_turn is an already-admitted Messages incomplete subtype, not
		// permission to pass arbitrary provider reasons into client wire output.
		if strings.EqualFold(strings.TrimSpace(completion.Reason()), "pause_turn") {
			return "pause_turn", nil
		}
		return "max_tokens", nil
	case canonical.CompletionDeclined:
		return "refusal", nil
	case canonical.CompletionFailed:
		return "", canonical.NewBackendError("", 0, "backend response failed: "+completion.Reason(), "")
	default:
		return "", canonical.InternalError("canonical completion class cannot be projected to Messages")
	}
}
