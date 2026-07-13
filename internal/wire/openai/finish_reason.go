package openai

import "strings"

const contentFilterFinishReason = "content_filter"
const maxOutputTokensFinishReason = "max_output_tokens"
const refusalStopReason = "refusal"
const safetyFinishReason = "SAFETY"
const guardrailIntervenedStopReason = "guardrail_intervened"
const contentFilteredStopReason = "content_filtered"

// IsContentFilterFinishReason reports whether a finish reason means the
// provider blocked the completion before producing usable output.
func IsContentFilterFinishReason(reason string) bool {
	return strings.EqualFold(strings.TrimSpace(reason), contentFilterFinishReason) // swobu:io-string source=boundary
}

// IsTerminalDeclineReason reports whether the terminal reason represents a
// blocked or refused generation outcome.
func IsTerminalDeclineReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) { // swobu:io-string source=boundary
	case contentFilterFinishReason, refusalStopReason, strings.ToLower(safetyFinishReason), guardrailIntervenedStopReason, contentFilteredStopReason:
		return true
	default:
		return false
	}
}

// IsTerminalIncompleteReason reports whether the terminal reason should lower
// to an incomplete response shape rather than a completed one.
func IsTerminalIncompleteReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) { // swobu:io-string source=boundary
	case contentFilterFinishReason, maxOutputTokensFinishReason:
		return true
	default:
		return false
	}
}
