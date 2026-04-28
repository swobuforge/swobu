package clientprofile

import "strings"

func openAIBaseURL(baseURL string) string {
	base := strings.TrimSpace(baseURL)
	if base == "" || base == "none" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/v1"
}
