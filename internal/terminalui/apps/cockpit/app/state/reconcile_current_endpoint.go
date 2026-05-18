package state

import "strings"

func reconcileCurrentEndpoint(current string, endpoints []string, hadEndpoints bool) string {
	trimmed := strings.TrimSpace(current) // swobu:io-string source=boundary
	if trimmed == "" {
		if hadEndpoints {
			return ""
		}
		return firstOrEmpty(endpoints)
	}
	if containsString(endpoints, trimmed) {
		return trimmed
	}
	return firstOrEmpty(endpoints)
}
