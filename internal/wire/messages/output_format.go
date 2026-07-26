package messages

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// Messages does not have an exact structured-output field in the current
// canonical band, so explicit structured-output requests must fail closed.
func rejectMessagesStructuredOutput(raw json.RawMessage) error {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	return canonical.NotImplemented("Swobu cannot yet preserve Messages structured output")
}

func rejectMessagesOutputFormat(format canonical.OutputFormat) error {
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
		return provider.NewIncompatibleTarget("Messages cannot represent canonical structured output")
	}
	return provider.NewIncompatibleTarget("Messages cannot represent the canonical output format")
}
