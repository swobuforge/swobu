package responses

import (
	"strings"
)

const responsesContentFilterReason = "content_filter"

func responsesTerminalStatus(eventType string, primaryStatus string, fallbackStatus string) string {
	status := strings.TrimSpace(primaryStatus)
	if status == "" {
		status = strings.TrimSpace(fallbackStatus)
	}
	if status != "" {
		return status
	}
	if strings.EqualFold(strings.TrimSpace(eventType), "response.incomplete") {
		return "incomplete"
	}
	return "completed"
}

func responsesTerminalReason(eventType string, primaryStatus string, fallbackStatus string, filters []responsesContentFilterDTO, incompleteReason string) (string, bool) {
	blockedSource := responsesBlockedContentFilterSource(filters)
	switch blockedSource {
	case "prompt", "mixed":
		return responsesContentFilterReason, true
	case "completion":
		return responsesContentFilterReason, false
	}
	reason := strings.TrimSpace(incompleteReason)
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
		source := strings.ToLower(strings.TrimSpace(filter.SourceType))
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
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "prompt":
		return "provider input was blocked by content filter"
	case "completion":
		return "provider output was blocked by content filter"
	default:
		return "provider response was blocked by content filter"
	}
}

func responseIncompleteReason(details *responsesIncompleteDetailsDTO) string {
	if details == nil {
		return ""
	}
	return details.Reason
}
