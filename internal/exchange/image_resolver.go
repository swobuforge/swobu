package exchange

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/url"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

type mediaFetchCache map[string]provider.InspectedImage

// newImageResolver returns the exchange-owned read-through capability passed
// into provider encoding. Only a byte-only codec invokes it; URL-native codecs
// preserve the canonical locator without causing Swobu network I/O.
func newImageResolver(
	policy provider.ImageFetchPolicy,
	limits provider.MediaLimits,
	fetcher provider.ImageFetcher,
	fetchCache *mediaFetchCache,
) func(context.Context, canonical.URLImage) (provider.InspectedImage, error) {
	requests := 0
	var totalBytes int64
	return func(ctx context.Context, urlImage canonical.URLImage) (provider.InspectedImage, error) {
		requests++
		if limits.MaxImages > 0 && requests > limits.MaxImages {
			return provider.InspectedImage{}, canonical.BadRequest("request image count exceeds limit")
		}
		network, materializationEnabled := policy.NetworkPolicy()
		if !materializationEnabled {
			return provider.InspectedImage{}, canonical.NotImplemented("this Swobu deployment has URL image materialization disabled")
		}
		inspected, exists := (*fetchCache)[urlImage.String()]
		if !exists {
			if fetcher == nil {
				return provider.InspectedImage{}, canonical.InternalError("URL image materialization requires a fetcher")
			}
			fetched, fetchErr := fetcher.FetchImage(ctx, urlImage, network, limits.MaxImageBytes)
			if fetchErr != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return provider.InspectedImage{}, ctxErr
				}
				if errors.Is(fetchErr, context.Canceled) {
					return provider.InspectedImage{}, fetchErr
				}
				logImageFetchFailure(urlImage, fetchErr)
				return provider.InspectedImage{}, canonical.BadRequest("URL image could not be materialized")
			}
			var inspectErr error
			inspected, inspectErr = provider.InspectImage(fetched.DeclaredMediaType, fetched.Bytes, limits)
			if inspectErr != nil {
				return provider.InspectedImage{}, canonical.BadRequest("materialized URL image is invalid: " + inspectErr.Error())
			}
			(*fetchCache)[urlImage.String()] = cloneInspectedImage(inspected)
		}
		totalBytes += int64(len(inspected.Bytes))
		if limits.MaxTotalImageBytes > 0 && totalBytes > limits.MaxTotalImageBytes {
			return provider.InspectedImage{}, canonical.BadRequest("request aggregate image bytes exceed limit")
		}
		return cloneInspectedImage(inspected), nil
	}
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
