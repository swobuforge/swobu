package custom

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
)

func classifyBackendError(err compatibility.BackendError) error {
	if isStrictToolModeUnsupported(err.Message) {
		return compatibility.NewClassifiedBackendError(compatibility.BackendErrorClassToolChoiceUnsupported, err)
	}
	return err
}

func isStrictToolModeUnsupported(raw string) bool {
	fields, ok := decodeBackendErrorFields(raw)
	if !ok {
		return false
	}
	message := strings.ToLower(fields.message)
	param := fields.param
	code := fields.code

	if param == "tool_choice" {
		switch code {
		case "unsupported_parameter", "unsupported_value", "invalid_value":
			return true
		}
		return strings.Contains(message, "tool_choice")
	}
	// OpenRouter-style provider-routing rejection for strict tool_choice values.
	if strings.Contains(message, "support the provided 'tool_choice' value") {
		return true
	}
	return false
}

type backendErrorFields struct {
	param   string
	code    string
	message string
}

func decodeBackendErrorFields(raw string) (backendErrorFields, bool) {
	var envelope struct {
		Error map[string]json.RawMessage `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &envelope) != nil || envelope.Error == nil {
		return backendErrorFields{}, false
	}
	return backendErrorFields{
		param:   decodeJSONFieldString(envelope.Error["param"]),
		code:    decodeJSONFieldString(envelope.Error["code"]),
		message: strings.TrimSpace(decodeJSONFieldString(envelope.Error["message"])),
	}, true
}

func decodeJSONFieldString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var out string
	if json.Unmarshal(raw, &out) != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
