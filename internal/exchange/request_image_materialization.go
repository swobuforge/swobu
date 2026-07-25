package exchange

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
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

type mediaMaterializationState struct {
	count      int
	totalBytes int64
	fetchCache mediaFetchCache
	used       session.ResolvedMedia
}

// materializeRequestImages resolves image carriers without consulting target
// grammar. The URL cache is exchange-transient; used contains only durable
// occurrence bindings supplied to this attempted request.
func materializeRequestImages(
	ctx context.Context,
	request canonical.CanonicalRequest,
	policy provider.ImageFetchPolicy,
	limits provider.MediaLimits,
	fetcher provider.ImageFetcher,
	fetchCache mediaFetchCache,
	historical session.ResolvedMedia,
) (canonical.CanonicalRequest, mediaFetchCache, session.ResolvedMedia, []compat.Decision, error) {
	state := mediaMaterializationState{fetchCache: cloneMediaFetchCache(fetchCache)}
	decisions := make([]compat.Decision, 0)
	prepared, err := canonical.RewriteRequestImages(request, func(position canonical.RequestPartRef, placement canonical.ImagePlacement, image canonical.ImagePart) (canonical.ImagePart, error) {
		state.count++
		if limits.MaxImages > 0 && state.count > limits.MaxImages {
			return canonical.ImagePart{}, canonical.BadRequest("request image count exceeds limit")
		}
		if inline, ok := image.Source().Inline(); ok {
			inspected, inspectErr := provider.InspectImage(inline.MediaType(), inline.Data(), limits)
			if inspectErr != nil {
				return canonical.ImagePart{}, imageMaterializationError(
					position,
					canonical.BadRequest("inline image bytes are invalid: "+inspectErr.Error()),
				)
			}
			if err := accountPreparedBytes(&state, int64(len(inspected.Bytes)), limits); err != nil {
				return canonical.ImagePart{}, err
			}
			materialized, err := canonical.NewInlineImage(inspected.MediaType, inspected.Bytes, image.Detail())
			if err != nil {
				return canonical.ImagePart{}, imageMaterializationError(
					position,
					canonical.InternalError("inspected inline image could not be reconstructed"),
				)
			}
			return materialized, nil
		}
		urlImage, ok := image.Source().URL()
		if !ok {
			return canonical.ImagePart{}, imageMaterializationError(
				position,
				canonical.InternalError("canonical image carrier is structurally invalid"),
			)
		}

		// A durable occurrence binding is historical invocation truth and must
		// prevent a mutable URL from being fetched again.
		if asset, exists := historical.Resolve(position, urlImage.String()); exists {
			return bindPreparedAsset(&state, position, urlImage.String(), image, asset, limits, &decisions, placement)
		}
		network, materializationEnabled := policy.NetworkPolicy()
		if !materializationEnabled {
			return canonical.ImagePart{}, imageMaterializationError(
				position,
				canonical.NotImplemented("this Swobu deployment has URL image materialization disabled"),
			)
		}
		inspected, exists := state.fetchCache[urlImage.String()]
		if !exists {
			if fetcher == nil {
				return canonical.ImagePart{}, imageMaterializationError(
					position,
					canonical.InternalError("URL image materialization requires a fetcher"),
				)
			}
			fetched, fetchErr := fetcher.FetchImage(ctx, urlImage, network, limits.MaxImageBytes)
			if fetchErr != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return canonical.ImagePart{}, ctxErr
				}
				if errors.Is(fetchErr, context.Canceled) {
					return canonical.ImagePart{}, fetchErr
				}
				logImageFetchFailure(urlImage, fetchErr)
				return canonical.ImagePart{}, imageMaterializationError(
					position,
					canonical.BadRequest("URL image could not be materialized"),
				)
			}
			var inspectErr error
			inspected, inspectErr = provider.InspectImage(fetched.DeclaredMediaType, fetched.Bytes, limits)
			if inspectErr != nil {
				return canonical.ImagePart{}, imageMaterializationError(
					position,
					canonical.BadRequest("materialized URL image is invalid: "+inspectErr.Error()),
				)
			}
			state.fetchCache[urlImage.String()] = cloneInspectedImage(inspected)
		}
		return bindPreparedImage(&state, position, urlImage.String(), image, inspected, limits, &decisions, placement)
	})
	return prepared, state.fetchCache, state.used, decisions, err
}

// logImageFetchFailure retains enough internal failure shape to diagnose media
// policy and reachability without logging paths, query parameters, or raw
// transport errors, all of which may contain caller secrets.
func logImageFetchFailure(source canonical.URLImage, err error) {
	parsed, parseErr := url.Parse(source.String())
	host := ""
	if parseErr == nil {
		host = parsed.Hostname()
	}
	slog.Debug("image materialization fetch failed",
		"component", "exchange",
		"event", "image_materialization_fetch_failed",
		"source_host", host,
		"failure_category", imageFetchFailureCategory(err),
	)
}

func imageFetchFailureCategory(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		if networkError.Timeout() {
			return "timeout"
		}
		return "network"
	}
	return "fetch"
}

func bindPreparedAsset(state *mediaMaterializationState, position canonical.RequestPartRef, sourceURL string, original canonical.ImagePart, asset session.ResolvedMediaAsset, limits provider.MediaLimits, decisions *[]compat.Decision, placement canonical.ImagePlacement) (canonical.ImagePart, error) {
	inspected, err := provider.InspectImage(asset.MediaType(), asset.Bytes(), limits)
	if err != nil {
		return canonical.ImagePart{}, imageMaterializationError(
			position,
			canonical.InternalError("resolved checkpoint media is corrupt: "+err.Error()),
		)
	}
	return bindPreparedImage(state, position, sourceURL, original, inspected, limits, decisions, placement)
}

func bindPreparedImage(state *mediaMaterializationState, position canonical.RequestPartRef, sourceURL string, original canonical.ImagePart, inspected provider.InspectedImage, limits provider.MediaLimits, decisions *[]compat.Decision, placement canonical.ImagePlacement) (canonical.ImagePart, error) {
	if err := accountPreparedBytes(state, int64(len(inspected.Bytes)), limits); err != nil {
		return canonical.ImagePart{}, err
	}
	used, err := state.used.Bind(position, sourceURL, inspected.MediaType, inspected.Bytes)
	if err != nil {
		return canonical.ImagePart{}, imageMaterializationError(
			position,
			canonical.InternalError("resolved media occurrence binding is invalid: "+err.Error()),
		)
	}
	state.used = used
	feature := compat.RequestItemsMessageImageSourceURL
	if placement == canonical.ImageInToolResult {
		feature = compat.RequestItemsToolResultImageSourceURL
	}
	*decisions = append(*decisions, compat.Decision{Feature: feature, Outcome: compat.Exact, Subject: mediaSubject(position)})
	materialized, err := canonical.NewInlineImage(inspected.MediaType, inspected.Bytes, original.Detail())
	if err != nil {
		return canonical.ImagePart{}, imageMaterializationError(
			position,
			canonical.InternalError("inspected URL image could not be reconstructed"),
		)
	}
	return materialized, nil
}

func accountPreparedBytes(state *mediaMaterializationState, size int64, limits provider.MediaLimits) error {
	state.totalBytes += size
	if limits.MaxTotalImageBytes > 0 && state.totalBytes > limits.MaxTotalImageBytes {
		return canonical.BadRequest("request aggregate image bytes exceed limit")
	}
	return nil
}

func imageMaterializationError(position canonical.RequestPartRef, err error) error {
	return fmt.Errorf(
		"image at request item %d part %d: %w",
		position.Item,
		position.Part,
		err,
	)
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
