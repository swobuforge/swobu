package messages

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ParseBackendError promotes only a recognized Messages error envelope.
func ParseBackendError(opaque canonical.BackendError) canonical.BackendError {
	var envelope struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Error     *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(opaque.Message), &envelope) != nil || envelope.Type != "error" || envelope.Error == nil || strings.TrimSpace(envelope.Error.Message) == "" {
		return opaque
	}
	detail := canonical.BackendErrorDetail{
		Type: strings.TrimSpace(envelope.Error.Type), Message: strings.TrimSpace(envelope.Error.Message),
		RequestID: strings.TrimSpace(envelope.RequestID),
	}
	structured := canonical.NewStructuredBackendError(opaque.TargetID, protocolkind.Messages, opaque.StatusCode, detail, opaque.RetryAfterHeaderValue)
	structured.Message = opaque.Message
	return structured
}
