package chatgptlogin

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func canonicalPublicBaseURL(raw string) string {
	base := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if base == "" {
		base = defaultPublicBaseURL
	}
	u, err := url.Parse(base)
	if err != nil || strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" { // swobu:io-string source=boundary
		return defaultPublicBaseURL
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	return strings.TrimRight(u.String(), "/")
}

func normalizeSessionID(raw string) string {
	id := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if id == "" || len(id) > 128 {
		return ""
	}
	for _, r := range id {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isAlphaNum && r != '-' && r != '_' {
			return ""
		}
	}
	return id
}

func canonicalAuthMode(raw string) string {
	mode := strings.ToLower(strings.TrimSpace(raw)) // swobu:io-string source=boundary
	if mode == "device" {
		return "device"
	}
	if mode == "browser" {
		return "browser"
	}
	return ""
}

func formatRemoteAuthError(raw []byte) string {
	message := strings.TrimSpace(extractRemoteAuthErrorMessage(raw)) // swobu:io-string source=boundary
	if message == "" {
		return ""
	}
	return ": " + message
}

func extractRemoteAuthErrorMessage(raw []byte) string {
	type envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	var decoded envelope
	if err := json.Unmarshal(raw, &decoded); err == nil {
		code := strings.TrimSpace(decoded.Error.Code)       // swobu:io-string source=boundary
		message := strings.TrimSpace(decoded.Error.Message) // swobu:io-string source=boundary
		if code == "" {
			code = strings.TrimSpace(decoded.Code) // swobu:io-string source=boundary
		}
		if message == "" {
			message = strings.TrimSpace(decoded.Message) // swobu:io-string source=boundary
		}
		if code != "" && message != "" {
			return fmt.Sprintf("%s (%s)", message, code)
		}
		if message != "" {
			return message
		}
		if code != "" {
			return code
		}
	}
	fallback := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if fallback == "" {
		return ""
	}
	if len(fallback) > 220 {
		fallback = fallback[:220]
	}
	return fallback
}
