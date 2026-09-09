package protocolcodec

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	messageswire "github.com/swobuforge/swobu/internal/wire/messages"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
)

// ParseBackendError delegates a bounded HTTP transport capture to the selected
// protocol owner. Unrecognized protocols and envelopes remain opaque.
func ParseBackendError(opaque canonical.BackendError, source protocolkind.ProtocolKind, requestID string) canonical.BackendError {
	switch source {
	case protocolkind.Messages:
		return messageswire.ParseBackendError(opaque)
	case protocolkind.Responses, protocolkind.ChatCompletions:
		return openaiwire.ParseBackendError(opaque, source, requestID)
	default:
		return opaque
	}
}
