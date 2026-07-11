package exchange

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type fallbackAttemptRunner func(context.Context, RouteAttempt) (TransportResponse, error)

// FallbackVendor executes ordered route attempts and only advances on
// pre-commit failures that remain fallbackable.
type FallbackVendor struct {
	CanFallback func(error) bool
}

// Execute runs ordered attempts until one succeeds or the ordered fallback is
// exhausted.
func (f FallbackVendor) Execute(ctx context.Context, attempts []RouteAttempt, run fallbackAttemptRunner) (TransportResponse, RouteAttempt, error) {
	if run == nil {
		return TransportResponse{}, RouteAttempt{}, canonical.InternalError("exchange fallback attempt runner is required")
	}
	if len(attempts) == 0 {
		return TransportResponse{}, RouteAttempt{}, canonical.InternalError("no viable route attempt")
	}
	canFallback := f.CanFallback
	if canFallback == nil {
		canFallback = canFallbackOnExchangeError
	}
	for i, attempt := range attempts {
		response, err := run(ctx, attempt)
		if err == nil {
			return response, attempt, nil
		}
		if i < len(attempts)-1 && canFallback(err) {
			continue
		}
		return TransportResponse{}, RouteAttempt{}, err
	}
	return TransportResponse{}, RouteAttempt{}, canonical.InternalError("no viable route attempt")
}
