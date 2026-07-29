package bedrock

import (
	"context"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// mantleMessagesCodec owns representation constraints imposed by the Bedrock
// Mantle Messages surface before the provider-neutral Messages grammar
// serializes a request. Chat Completions and Responses retain their own
// protocol semantics; applying this restriction to them would reject
// representable requests at the wrong target scope.
type mantleMessagesCodec struct {
	provider.Codec
}

func (c mantleMessagesCodec) Encode(request provider.Request) (carrier.Document, []compat.Decision, error) {
	if request.Canonical.OutputFormatSpecified() {
		format := request.Canonical.OutputFormat()
		if !format.IsZero() && format.Kind != canonical.OutputFormatText {
			return carrier.Document{}, nil, provider.NewIncompatibleTarget(
				"Bedrock Mantle Messages cannot represent native structured output",
			)
		}
	}
	return c.Codec.Encode(request)
}

func (c mantleMessagesCodec) Decode(ctx context.Context, request provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.Codec.Decode(ctx, request, ingress)
}
