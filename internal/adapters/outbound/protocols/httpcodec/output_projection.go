package httpcodec

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// OutputText concatenates textual output items in order for families that expose
// a single flat text field in their batch response shape.
func OutputText(items []canonical.OutputItem) string {
	out := ""
	for _, item := range items {
		if item.Kind != canonical.OutputItemText {
			continue
		}
		out += item.Text
	}
	return out
}

func ContainsToolUseOutput(items []canonical.OutputItem) bool {
	for _, item := range items {
		if item.Kind == canonical.OutputItemToolUse {
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
