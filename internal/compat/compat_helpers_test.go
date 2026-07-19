package compat

import (
	"fmt"
	"strings"
)

// ValidateSubject reports whether one decision subject matches the canonical
// subject grammar. Empty subjects are permitted.
func ValidateSubject(subject Subject) error {
	normalized := strings.TrimSpace(string(subject))
	if normalized == "" {
		return nil
	}
	prefix, locator, ok := strings.Cut(normalized, ":")
	if !ok || prefix == "" || locator == "" {
		return fmt.Errorf("subject %q is invalid", normalized)
	}
	switch prefix {
	case "wire":
		if !strings.HasPrefix(locator, "/") {
			return fmt.Errorf("subject %q is invalid", normalized)
		}
	case "canonical", "state", "event", "route", "provider":
	default:
		return fmt.Errorf("subject %q is invalid", normalized)
	}
	if strings.Contains(locator, " ") {
		return fmt.Errorf("subject %q is invalid", normalized)
	}
	return nil
}
