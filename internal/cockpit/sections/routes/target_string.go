package routes

import (
	"errors"
	"strings"
)

// FormatTarget renders a provider/model pair as the canonical UI string.
// Empty provider or model is preserved as-is (one side may be empty during
// drafting).
func FormatTarget(provider, model string) string {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" {
		return model
	}
	if model == "" {
		return provider
	}
	return provider + "/" + model
}

// ParseTarget splits a "provider/model" UI string into provider and model.
// The first slash separates provider from model; everything after the first
// slash is the model, even if it contains additional slashes.
//
// Errors:
//   - empty string
//   - no slash present (single word is treated as invalid in the target
//     string context; caller may decide to interpret bare provider differently)
func ParseTarget(raw string) (provider, model string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", errors.New("target required")
	}
	parts := strings.SplitN(raw, "/", 2)
	provider = strings.TrimSpace(parts[0])
	if provider == "" {
		return "", "", errors.New("provider required")
	}
	if len(parts) != 2 {
		return "", "", errors.New("use provider/model, for example openai/gpt-4.1")
	}
	model = strings.TrimSpace(parts[1])
	if model == "" {
		return "", "", errors.New("model required")
	}
	return provider, model, nil
}
