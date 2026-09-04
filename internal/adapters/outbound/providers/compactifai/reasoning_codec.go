package compactifai

import (
	"context"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/provider"
)

type reasoningCodec struct{ standard protocolcodec.Codec }

func (c reasoningCodec) Encode(request provider.Request) (carrier.Document, []compat.Change, error) {
	return c.standard.Encode(request)
}

func (c reasoningCodec) Decode(ctx context.Context, request provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return protocolcodec.DecodeChatWithReasoningCarrier(ctx, c.standard, request, ingress, protocolcodec.ReasoningContentExtractor{})
}

var _ provider.Codec = reasoningCodec{}
