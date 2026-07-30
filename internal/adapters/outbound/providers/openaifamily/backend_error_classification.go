package openaifamily

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func classifyBackendError(err canonical.BackendError) error {
	if isStrictToolModeUnsupported(err.Message) {
		return canonical.NewClassifiedBackendError(canonical.BackendErrorClassToolChoiceUnsupported, err)
	}
	if isContinuationReferenceInvalid(err) {
		return canonical.NewClassifiedBackendError(canonical.BackendErrorClassContinuationReferenceInvalid, err)
	}
	return err
}

func isContinuationReferenceInvalid(err canonical.BackendError) bool {
	if err.StatusCode != 400 && err.StatusCode != 404 {
		return false
	}
	fields, ok := decodeBackendErrorFields(err.Message)
	if !ok || fields.param != "previous_response_id" {
		return false
	}
	switch strings.TrimSpace(fields.code) {
	case "invalid_value", "not_found", "previous_response_not_found":
		return true
	default:
		return strings.Contains(strings.ToLower(fields.message), "previous_response")
	}
}

func isStrictToolModeUnsupported(raw string) bool {
	fields, ok := decodeBackendErrorFields(raw)
	if !ok {
		return false
	}
	message := strings.ToLower(fields.message) // swobu:io-string source=boundary
	param := fields.param
	code := strings.TrimSpace(fields.code) // swobu:io-string source=provider-wire

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
		message: strings.TrimSpace(decodeJSONFieldString(envelope.Error["message"])), // swobu:io-string source=boundary
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
	return strings.TrimSpace(out) // swobu:io-string source=boundary
}
