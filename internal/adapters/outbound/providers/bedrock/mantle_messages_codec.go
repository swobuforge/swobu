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

func (c mantleMessagesCodec) Encode(request provider.Request) (carrier.Document, []compat.Change, error) {
	resolved, err := canonical.RewriteRequestImages(request.Canonical, func(_ canonical.RequestPartRef, _ canonical.ImagePlacement, image canonical.ImagePart) (canonical.ImagePart, error) {
		urlImage, ok := image.Source().URL()
		if !ok {
			return image, nil
		}
		if request.EncodeContext.ResolveImage == nil {
			return canonical.ImagePart{}, canonical.InternalError("Bedrock Mantle Messages requires an image resolver for URL images")
		}
		ctx := request.EncodeContext.Context
		if ctx == nil {
			ctx = context.Background()
		}
		asset, err := request.EncodeContext.ResolveImage(ctx, urlImage)
		if err != nil {
			return canonical.ImagePart{}, err
		}
		return canonical.NewInlineImage(asset.MediaType, asset.Bytes, image.Detail())
	})
	if err != nil {
		return carrier.Document{}, nil, err
	}
	request.Canonical = resolved
	return c.Codec.Encode(request)
}

func (c mantleMessagesCodec) Decode(ctx context.Context, request provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.Codec.Decode(ctx, request, ingress)
}
