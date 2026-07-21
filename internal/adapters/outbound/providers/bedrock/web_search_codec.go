package bedrock

import (
	"context"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// webSearchBackendCodec fails closed because no configured Bedrock protocol surface in
// this package owns native provider-hosted web-search lowering.
type webSearchBackendCodec struct{ standard protocolcodec.Codec }

func (c webSearchBackendCodec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	for _, tool := range req.Canonical.Tools() {
		if tool.Kind() == canonical.ToolKindWebSearch {
			return carrier.Document{}, nil, provider.UnsupportedByBackend(canonical.UnsupportedOperation("Bedrock target does not support provider-hosted web search"))
		}
	}
	return c.standard.Encode(req)
}

func (c webSearchBackendCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.standard.Decode(ctx, req, ingress)
}

var _ provider.Codec = webSearchBackendCodec{}
