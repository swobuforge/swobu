package model

import (
	"fmt"
	"net/url"
	"strings"
)

func BedrockRegionFromBaseURL(baseURL string) string {
	trimmed := strings.TrimSpace(baseURL) // swobu:io-string source=boundary
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	host := strings.TrimSpace(strings.ToLower(u.Hostname())) // swobu:io-string source=boundary
	parts := strings.Split(host, ".")
	if len(parts) >= 4 && strings.HasPrefix(parts[0], "bedrock-runtime") {
		return strings.TrimSpace(parts[1]) // swobu:io-string source=boundary
	}
	return ""
}

func BedrockBaseURLForRegion(region string) string {
	region = strings.TrimSpace(region) // swobu:io-string source=boundary
	if region == "" {
		return ""
	}
	return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/openai/v1", region)
}
