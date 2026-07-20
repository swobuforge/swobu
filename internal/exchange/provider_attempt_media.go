package exchange

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/replay"
)

func requestHasImages(request canonical.CanonicalRequest) bool {
	found := false
	_ = canonical.WalkRequestImages(request, func(canonical.RequestPartRef, canonical.ImagePlacement, canonical.ImagePart) error {
		found = true
		return nil
	})
	return found
}

type mediaFetchCache map[string]provider.InspectedImage

type mediaPreparationState struct {
	count      int
	totalBytes int64
	fetchCache mediaFetchCache
	used       replay.ResolvedMedia
}

// prepareImages resolves one candidate request. The URL cache is transient;
// used contains only occurrence bindings supplied to this attempted request.
func prepareImages(
	ctx context.Context,
	request canonical.CanonicalRequest,
	protocol protocolkind.ProtocolKind,
	policy provider.ImageFetchPolicy,
	limits provider.MediaLimits,
	fetcher provider.ImageFetcher,
	fetchCache mediaFetchCache,
	historical replay.ResolvedMedia,
) (canonical.CanonicalRequest, mediaFetchCache, replay.ResolvedMedia, []compat.Decision, error) {
	state := mediaPreparationState{fetchCache: cloneMediaFetchCache(fetchCache)}
	decisions := make([]compat.Decision, 0)
	prepared, err := canonical.RewriteRequestImages(request, func(position canonical.RequestPartRef, placement canonical.ImagePlacement, image canonical.ImagePart) (canonical.ImagePart, error) {
		state.count++
		if limits.MaxImages > 0 && state.count > limits.MaxImages {
			return canonical.ImagePart{}, preparationError(PreparationRequest, "request image count exceeds limit")
		}
		if !provider.SupportsImagePlacement(protocol, placement) {
			return canonical.ImagePart{}, preparationError(PreparationCandidate, "protocol cannot preserve %s image at request item %d part %d", placement, position.Item, position.Part)
		}
		if inline, ok := image.Source().Inline(); ok {
			inspected, inspectErr := provider.InspectImage(inline.MediaType(), inline.Data(), limits)
			if inspectErr != nil {
				return canonical.ImagePart{}, preparationError(PreparationRequest, "inline image at request item %d part %d: %w", position.Item, position.Part, inspectErr)
			}
			if err := accountPreparedBytes(&state, int64(len(inspected.Bytes)), limits); err != nil {
				return canonical.ImagePart{}, err
			}
			return canonical.NewInlineImage(inspected.MediaType, inspected.Bytes, image.Detail())
		}
		urlImage, ok := image.Source().URL()
		if !ok {
			return canonical.ImagePart{}, preparationError(PreparationRequest, "canonical image carrier is invalid")
		}

		// A durable occurrence binding is historical invocation truth and must
		// prevent a mutable URL from being fetched again.
		if asset, exists := historical.Resolve(position, urlImage.String()); exists {
			return bindPreparedAsset(&state, position, urlImage.String(), image, asset, limits, &decisions, placement)
		}
		network, materializationEnabled := policy.NetworkPolicy()
		if !materializationEnabled {
			return canonical.ImagePart{}, preparationError(PreparationRequest, "URL image materialization is disabled")
		}
		inspected, exists := state.fetchCache[urlImage.String()]
		if !exists {
			if fetcher == nil {
				return canonical.ImagePart{}, preparationError(PreparationRequest, "URL image materialization requires a fetcher")
			}
			fetched, fetchErr := fetcher.FetchImage(ctx, urlImage, network, limits.MaxImageBytes)
			if fetchErr != nil {
				return canonical.ImagePart{}, preparationError(PreparationRequest, "materialize URL image: %w", fetchErr)
			}
			var inspectErr error
			inspected, inspectErr = provider.InspectImage(fetched.DeclaredMediaType, fetched.Bytes, limits)
			if inspectErr != nil {
				return canonical.ImagePart{}, preparationError(PreparationRequest, "inspect fetched URL image: %w", inspectErr)
			}
			state.fetchCache[urlImage.String()] = cloneInspectedImage(inspected)
		}
		return bindPreparedImage(&state, position, urlImage.String(), image, inspected, limits, &decisions, placement)
	})
	return prepared, state.fetchCache, state.used, decisions, err
}

func bindPreparedAsset(state *mediaPreparationState, position canonical.RequestPartRef, sourceURL string, original canonical.ImagePart, asset replay.ResolvedMediaAsset, limits provider.MediaLimits, decisions *[]compat.Decision, placement canonical.ImagePlacement) (canonical.ImagePart, error) {
	inspected, err := provider.InspectImage(asset.MediaType(), asset.Bytes(), limits)
	if err != nil {
		return canonical.ImagePart{}, preparationError(PreparationRequest, "resolved replay media is invalid: %w", err)
	}
	return bindPreparedImage(state, position, sourceURL, original, inspected, limits, decisions, placement)
}

func bindPreparedImage(state *mediaPreparationState, position canonical.RequestPartRef, sourceURL string, original canonical.ImagePart, inspected provider.InspectedImage, limits provider.MediaLimits, decisions *[]compat.Decision, placement canonical.ImagePlacement) (canonical.ImagePart, error) {
	if err := accountPreparedBytes(state, int64(len(inspected.Bytes)), limits); err != nil {
		return canonical.ImagePart{}, err
	}
	used, err := state.used.Bind(position, sourceURL, inspected.MediaType, inspected.Bytes)
	if err != nil {
		return canonical.ImagePart{}, preparationError(PreparationRequest, "%w", err)
	}
	state.used = used
	feature := compat.RequestItemsMessageImageSourceURL
	if placement == canonical.ImageInToolResult {
		feature = compat.RequestItemsToolResultImageSourceURL
	}
	*decisions = append(*decisions, compat.Decision{Feature: feature, Outcome: compat.Exact, Subject: mediaSubject(position)})
	return canonical.NewInlineImage(inspected.MediaType, inspected.Bytes, original.Detail())
}

func accountPreparedBytes(state *mediaPreparationState, size int64, limits provider.MediaLimits) error {
	state.totalBytes += size
	if limits.MaxTotalImageBytes > 0 && state.totalBytes > limits.MaxTotalImageBytes {
		return preparationError(PreparationRequest, "request aggregate image bytes exceed limit")
	}
	return nil
}

func mediaSubject(position canonical.RequestPartRef) compat.Subject {
	return compat.Subject(fmt.Sprintf("request.items[%d].content[%d]", position.Item, position.Part))
}

func cloneMediaFetchCache(values mediaFetchCache) mediaFetchCache {
	out := make(mediaFetchCache, len(values))
	for key, value := range values {
		out[key] = cloneInspectedImage(value)
	}
	return out
}

func cloneInspectedImage(image provider.InspectedImage) provider.InspectedImage {
	image.Bytes = append([]byte(nil), image.Bytes...)
	return image
}
