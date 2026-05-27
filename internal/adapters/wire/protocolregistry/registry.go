package protocolregistry

import (
	"io"

	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	chatcompletions "github.com/swobuforge/swobu/internal/adapters/wire/protocols/chatcompletions"
	completions "github.com/swobuforge/swobu/internal/adapters/wire/protocols/completions"
	messages "github.com/swobuforge/swobu/internal/adapters/wire/protocols/messages"
	responses "github.com/swobuforge/swobu/internal/adapters/wire/protocols/responses"
	streamwire "github.com/swobuforge/swobu/internal/adapters/wire/shared/streamwire"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// FamilyCodec translates one protocol family between wire payloads and
// canonical request/output envelope surfaces.
type FamilyCodec interface {
	DecodeRequest(raw []byte) (canonical.CanonicalRequest, bool, error)
	EncodeResponse(output canonical.CanonicalOutput) ([]byte, error)
	NewStreamState() streamwire.EnvelopeStreamEncoder
}

// EgressCodec is the protocol runtime contract for provider-side request/response
// wire realization and decode.
type EgressCodec interface {
	EncodeRequest(request canonical.CanonicalRequest, streaming bool) (core.WireRequest, error)
	DecodeResponse(raw []byte) (canonical.CanonicalOutputValue, error)
	DecodeResponseStream(body io.ReadCloser, exchangeID string) canonical.EventReader
}

// ResponsesEncodeOptionsCodec is an optional extension for responses-specific
// encode options used by dedicated provider flows (for example codexwire).
type ResponsesEncodeOptionsCodec interface {
	EncodeRequestWithOptions(request canonical.CanonicalRequest, streaming bool, options ResponsesEncodeOptions) (core.WireRequest, error)
}

type ResponsesEncodeOptions = responses.EncodeOptions

func ForIngressFamily(family canonical.IngressFamily) (FamilyCodec, error) {
	switch family {
	case canonical.IngressFamilyChatCompletions:
		return chatcompletions.ChatCompletionsFamilyCodec{}, nil
	case canonical.IngressFamilyResponses:
		return responses.ResponsesFamilyCodec{}, nil
	case canonical.IngressFamilyCompletions:
		return completions.CompletionsFamilyCodec{}, nil
	case canonical.IngressFamilyMessages:
		return messages.MessagesFamilyCodec{}, nil
	default:
		return nil, canonical.UnsupportedOperation("ingress family is not implemented")
	}
}

func ForProtocolKind(kind protocolkind.ProtocolKind) (EgressCodec, error) {
	switch kind {
	case protocolkind.ChatCompletions:
		return chatcompletions.ChatCompletionsFamilyCodec{}, nil
	case protocolkind.Responses:
		return responses.ResponsesFamilyCodec{}, nil
	case protocolkind.Completions:
		return completions.CompletionsFamilyCodec{}, nil
	case protocolkind.Messages:
		return messages.MessagesFamilyCodec{}, nil
	default:
		return nil, canonical.UnsupportedOperation("protocol kind is not implemented")
	}
}
