package modelscope

import (
	"context"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/provider"
)

// reasoningCodec leaves standard Chat requests untouched and extracts only
// ModelScope's readable response trace.
type reasoningCodec struct{ standard protocolcodec.Codec }

func (c reasoningCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	return c.standard.Encode(req)
}

func (c reasoningCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return protocolcodec.DecodeChatWithReasoningCarrier(ctx, c.standard, req, ingress, protocolcodec.ReasoningContentExtractor{})
}

var _ provider.Codec = reasoningCodec{}
