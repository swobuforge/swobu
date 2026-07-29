package responses

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

const responsesContentFilterReason = "content_filter"

func responsesCompletion(status string, reason string) canonical.Completion {
	if normalizedResponseString(reason) == responsesContentFilterReason {
		return canonical.Declined(reason)
	}
	switch normalizedResponseString(status) {
	case "completed":
		return canonical.Completed(reason)
	case "incomplete":
		return canonical.Incomplete(reason)
	default:
		return canonical.Failed(reason)
	}
}

func trimmedResponseString(value string) string {
	return strings.TrimSpace(value) // swobu:io-string source=boundary
}

func normalizedResponseString(value string) string {
	return strings.ToLower(trimmedResponseString(value)) // swobu:io-string source=boundary
}

func responsesTerminalStatus(eventType string, primaryStatus string, fallbackStatus string) string {
	status := trimmedResponseString(primaryStatus)
	if status == "" {
		status = trimmedResponseString(fallbackStatus)
	}
	if status != "" {
		return status
	}
	if normalizedResponseString(eventType) == "response.incomplete" {
		return "incomplete"
	}
	return "completed"
}

func responsesTerminalReason(eventType string, primaryStatus string, fallbackStatus string, filters []responsesContentFilterDTO, incompleteReason string) (string, bool) {
	blockedSource := responsesBlockedContentFilterSource(filters)
	if blockedSource == "prompt" || blockedSource == "mixed" {
		return responsesContentFilterReason, true
	}
	if blockedSource == "completion" {
		return responsesContentFilterReason, false
	}
	reason := trimmedResponseString(incompleteReason)
	if reason != "" {
		return reason, false
	}
	status := responsesTerminalStatus(eventType, primaryStatus, fallbackStatus)
	return status, false
}

func responsesBlockedContentFilterSource(filters []responsesContentFilterDTO) string {
	sourceType := ""
	for _, filter := range filters {
		if !filter.Blocked {
			continue
		}
		source := normalizedResponseString(filter.SourceType)
		if source == "" {
			continue
		}
		if sourceType == "" {
			sourceType = source
			continue
		}
		if sourceType != source {
			return "mixed"
		}
	}
	return sourceType
}

func responsesContentFilterMessage(sourceType string) string {
	source := normalizedResponseString(sourceType)
	if source == "prompt" {
		return "provider input was blocked by content filter"
	}
	if source == "completion" {
		return "provider output was blocked by content filter"
	}
	return "provider response was blocked by content filter"
}

func responseIncompleteReason(details *responsesIncompleteDetailsDTO) string {
	if details == nil {
		return ""
	}
	return details.Reason
}
