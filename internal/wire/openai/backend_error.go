package openai

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// ParseBackendError promotes a bounded opaque transport capture only when it
// is a recognized OpenAI-family error envelope. Malformed and unfamiliar
// bodies remain opaque.
func ParseBackendError(opaque canonical.BackendError, source protocolkind.ProtocolKind, requestID string) canonical.BackendError {
	if source != protocolkind.Responses && source != protocolkind.ChatCompletions {
		return opaque
	}
	var envelope struct {
		Error *struct {
			Type    string  `json:"type"`
			Code    string  `json:"code"`
			Message string  `json:"message"`
			Param   *string `json:"param"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(opaque.Message), &envelope) != nil || envelope.Error == nil || strings.TrimSpace(envelope.Error.Message) == "" {
		return opaque
	}
	detail := canonical.BackendErrorDetail{
		Type: strings.TrimSpace(envelope.Error.Type), Code: strings.TrimSpace(envelope.Error.Code),
		Message: strings.TrimSpace(envelope.Error.Message), RequestID: strings.TrimSpace(requestID),
	}
	if envelope.Error.Param != nil {
		detail.Param = strings.TrimSpace(*envelope.Error.Param)
	}
	structured := canonical.NewStructuredBackendError(opaque.TargetID, source, opaque.StatusCode, detail, opaque.RetryAfterHeaderValue)
	structured.Message = opaque.Message
	return structured
}
