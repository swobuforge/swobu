package openrouter

import (
	"context"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/responses"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

// responsesCodec owns OpenRouter's exact Responses request composition while
// the standard codec retains shared response decoding.
type responsesCodec struct{ standard protocolcodec.Codec }

func (c responsesCodec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	document, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (responses.ProviderRequestDocument, error) {
		return responses.LowerProviderRequestDocument(
			responses.EncodeInput{Request: req.Canonical, Responses: req.Responses.Clone()},
			req.Delivery,
			sink,
			req.ExchangeID,
			responses.EncodeOptions{Compatibility: req.Compatibility},
		)
	})
	if err != nil {
		return carrier.Document{}, decisions, protocolcodec.MarkUnsupportedByBackend(err)
	}
	for index := range document.Tools {
		if document.Tools[index].Type == canonical.ToolTypeWebSearch {
			document.Tools[index].Type = "openrouter:web_search"
		}
	}
	encoded, err := responses.EncodeProviderRequestDocument(document)
	return encoded, decisions, err
}

func (c responsesCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.standard.Decode(ctx, req, ingress)
}

var _ provider.Codec = responsesCodec{}
