package chatgpt

import (
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

type backendCodec struct {
	protocolcodec.Codec
}

func newBackendCodec(_ string) backendCodec {
	return backendCodec{Codec: protocolcodec.Codec{Protocol: protocolkind.Responses}}
}

func (c backendCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	if req.Delivery != delivery.StreamingDelivery(delivery.FramingSSE) {
		return carrier.Document{}, nil, provider.NewIncompatibleTarget("ChatGPT target requires SSE streaming delivery")
	}
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	var changes []compat.Change
	document, err := func(sink *[]compat.Change) (responses.ProviderRequestDocument, error) {
		return responses.LowerProviderRequestDocument(
			responses.EncodeInput{Request: req.Canonical},
			req.Delivery,
			sink,
			req.ExchangeID,
			responses.EncodeOptions{},
		)
	}(&changes)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	if input, ok := document.Input.(string); ok {
		document.Input = []any{map[string]any{"type": "message", "role": "user", "content": input}}
	}
	store := false
	document.Store = &store
	encoded, err := responses.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

var _ provider.Codec = backendCodec{}
