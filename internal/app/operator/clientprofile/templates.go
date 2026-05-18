package clientprofile

import "strings"

func openAIBaseURL(baseURL string) string {
	base := strings.TrimSpace(baseURL) // swobu:io-string source=boundary
	if base == "" || base == "none" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/v1"
}
