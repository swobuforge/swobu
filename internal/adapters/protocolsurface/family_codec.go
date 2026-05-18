package protocolsurface

import (
	chatcompletions "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/chat_completions"
	completions "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/completions"
	httpcodec "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/httpcodec"
	messages "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/messages"
	responses "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/responses"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// FamilyCodec translates one protocol family between wire payloads and
// canonical request/output envelope surfaces.
type FamilyCodec interface {
	DecodeRequest(raw []byte) (canonical.CanonicalRequest, bool, error)
	EncodeBuffered(output canonical.CanonicalOutput) ([]byte, error)
	NewStreamState() httpcodec.EnvelopeStreamEncoder
}

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
