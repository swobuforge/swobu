package completions

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// Completions does not have an exact structured-output field in the current
// canonical band, so explicit structured-output requests must fail closed.
func rejectCompletionsStructuredOutput(raw json.RawMessage) error {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	return canonical.UnsupportedOperation("completions protocol does not support structured output on swobu v0")
}

func rejectCompletionsOutputFormat(format canonical.OutputFormat) error {
	if format.IsZero() {
		return nil
	}
	if err := format.Validate(); err != nil {
		return err
	}
	if format.Kind == canonical.OutputFormatText {
		return nil
	}
	if format.Kind == canonical.OutputFormatJSONSchema {
		return canonical.UnsupportedOperation("completions protocol does not support structured output on swobu v0")
	}
	return canonical.UnsupportedOperation("completions protocol does not support the requested output format")
}
