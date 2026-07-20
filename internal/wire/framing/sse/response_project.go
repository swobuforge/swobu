package sse

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// OutputText concatenates textual output items in order for families that expose
// a single flat text field in their batch response shape.
func OutputText(items []canonical.CanonicalItem) string {
	out := ""
	for _, item := range items {
		message, ok := item.Message()
		if !ok {
			continue
		}
		for _, part := range message.Content() {
			if text, ok := part.Text(); ok {
				out += text.Text()
			}
		}
	}
	return out
}

func ContainsToolUseOutput(items []canonical.CanonicalItem) bool {
	for _, item := range items {
		if item.Kind() == canonical.ItemKindToolCall {
			return true
		}
	}
	return false
}

func DefaultFinishReason(value string, fallback string) string {
	if strings.TrimSpace(value) == "" { // swobu:io-string source=boundary
		return fallback
	}
	return value
}

func FallbackID(value string, fallback string) string {
	if strings.TrimSpace(value) == "" { // swobu:io-string source=boundary
		return fallback
	}
	return value
}
