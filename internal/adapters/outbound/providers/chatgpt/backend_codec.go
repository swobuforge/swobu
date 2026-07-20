package chatgpt

import (
	"context"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

type backendCodec struct {
	inner protocolcodec.Codec
}

func newBackendCodec(providerID string) backendCodec {
	store := false
	return backendCodec{inner: protocolcodec.Codec{
		ProviderID: providerID,
		Protocol:   protocolkind.Responses,
		Options: protocolcodec.Options{
			ForceStructuredInput: true,
			Store:                &store,
		},
	}}
}

func (c backendCodec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	if req.Delivery != delivery.StreamingDelivery(delivery.FramingSSE) {
		return carrier.Document{}, nil, canonical.UnsupportedDelivery("chatgpt provider requires SSE streaming delivery")
	}
	return c.inner.Encode(req)
}

func (c backendCodec) Decode(ctx context.Context, request provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.inner.Decode(ctx, request, ingress)
}

var _ provider.Codec = backendCodec{}
