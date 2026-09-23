package exchange

import (
	"context"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/wire"
)

// runLocalExchange is test-only convenience for package-level lifecycle tests.
// Production callers must always pass the identity established by admission.
func runLocalExchange(ctx context.Context, runner runtimeBundle, exchangeID string, clientHandler trafficevidence.ClientHandler, clientFamily canonical.ClientFamily, clientDelivery delivery.Delivery, decoded wire.ClientRequestResult, ingressChanges []compat.Change, workspace routing.Workspace, timing *trafficevidence.Timing, requestPath canonical.NormalizedPath, affinity executionaffinity.Key) (RequestOutput, error) {
	return runExchange(ctx, runner, accessidentity.Local, exchangeID, clientHandler, clientFamily, clientDelivery, decoded, ingressChanges, workspace, timing, requestPath, affinity)
}
