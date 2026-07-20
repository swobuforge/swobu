package chatgpt

import (
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

type backendCodec struct {
	protocolcodec.Codec
}

func newBackendCodec(providerID string) backendCodec {
	return backendCodec{Codec: protocolcodec.Codec{
		ProviderID: providerID,
		Protocol:   protocolkind.Responses,
	}}
}

func (c backendCodec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	if req.Delivery != delivery.StreamingDelivery(delivery.FramingSSE) {
		return carrier.Document{}, nil, canonical.UnsupportedDelivery("chatgpt provider requires SSE streaming delivery")
	}
	return c.Codec.EncodeResponses(req, func(document *responses.ProviderRequestDocument) error {
		if input, ok := document.Payload["input"].(string); ok {
			document.Payload["input"] = []any{map[string]any{"type": "message", "role": "user", "content": input}}
		}
		document.Payload["store"] = false
		return nil
	})
}

var _ provider.Codec = backendCodec{}
