package replay

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// CaptureRequest reconstructs the full semantic request that produced a provider
// response, so it can be stored as a replay record.
//
// If native is nil, the provider already received full canonical state; return
// the provider request with turn cleared.
//
// If native is present, the provider used a native ref for a delta request.
// Load the previous record and materialize the full request from it.
func CaptureRequest(
	ctx context.Context,
	store Store,
	scope Scope,
	native *NativeRef,
	providerRequest canonical.CanonicalRequest,
) (canonical.CanonicalRequest, error) {
	if native == nil {
		return withoutTurn(providerRequest), nil
	}

	previous, found, err := store.Get(ctx, scope, native.ReplayID)
	if err != nil {
		return canonical.CanonicalRequest{}, err
	}
	if !found {
		return canonical.CanonicalRequest{}, canonical.InternalError("native replay parent is missing")
	}

	return materialize(previous, providerRequest), nil
}
