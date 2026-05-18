package effect

import (
	"fmt"
	"regexp"
	"strings"
)

func normalizeAuthSessionSurfaceError(err error) string {
	raw := strings.TrimSpace(err.Error()) // swobu:io-string source=boundary
	lower := strings.ToLower(raw)         // swobu:io-string source=boundary
	if strings.Contains(lower, "auth session") && strings.Contains(lower, "code=") {
		return sanitizeAuthSessionErrorMessage(strings.TrimSpace(strings.TrimPrefix(raw, "operator client:"))) // swobu:io-string source=boundary
	}
	return sanitizeAuthSessionErrorMessage(normalizeOperatorSurfaceError(err))
}

var (
	authReturnedStatusPattern = regexp.MustCompile(`(?i)returned status\s+(\d{3})`)
	authCodePattern           = regexp.MustCompile(`(?i)\(code=([A-Z_]+)\)`)
)

func sanitizeAuthSessionErrorMessage(message string) string {
	trimmed := strings.TrimSpace(message) // swobu:io-string source=boundary
	if trimmed == "" {
		return trimmed
	}
	lower := strings.ToLower(trimmed) // swobu:io-string source=boundary
	if strings.Contains(lower, "<html") || strings.Contains(lower, "<!doctype html") {
		status := ""
		if match := authReturnedStatusPattern.FindStringSubmatch(trimmed); len(match) == 2 {
			status = strings.TrimSpace(match[1]) // swobu:io-string source=boundary
		}
		code := ""
		if match := authCodePattern.FindStringSubmatch(trimmed); len(match) == 2 {
			code = strings.TrimSpace(match[1]) // swobu:io-string source=boundary
		}
		summary := "auth start failed: upstream returned an HTML challenge page"
		if status != "" {
			summary = fmt.Sprintf("auth start failed: upstream returned status %s with an HTML challenge page", status)
		}
		if code != "" {
			summary = summary + " (code=" + code + ")"
		}
		return summary
	}
	if len(trimmed) > 240 {
		return strings.TrimSpace(trimmed[:240]) + "…" // swobu:io-string source=boundary
	}
	return trimmed
}

func authModeForCredentialRef(ref string) string {
	if strings.EqualFold(strings.TrimSpace(ref), "chatgpt_device_auth") { // swobu:io-string source=boundary
		return "device"
	}
	return "browser"
}
