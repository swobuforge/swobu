package chatgpt

import (
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/responses"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

type backendCodec struct {
	protocolcodec.Codec
}

func newBackendCodec(_ string) backendCodec {
	return backendCodec{Codec: protocolcodec.Codec{Protocol: protocolkind.Responses}}
}

func (c backendCodec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	if req.Delivery != delivery.StreamingDelivery(delivery.FramingSSE) {
		return carrier.Document{}, nil, provider.NewIncompatibleTarget("ChatGPT target requires SSE streaming delivery")
	}
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	document, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (responses.ProviderRequestDocument, error) {
		return responses.LowerProviderRequestDocument(
			responses.EncodeInput{Request: req.Canonical},
			req.Delivery,
			sink,
			req.ExchangeID,
			responses.EncodeOptions{},
		)
	})
	if err != nil {
		return carrier.Document{}, decisions, err
	}
	if input, ok := document.Input.(string); ok {
		document.Input = []any{map[string]any{"type": "message", "role": "user", "content": input}}
	}
	store := false
	document.Store = &store
	encoded, err := responses.EncodeProviderRequestDocument(document)
	return encoded, decisions, err
}

var _ provider.Codec = backendCodec{}
