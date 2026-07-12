package exchange

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// orderedFallbackExecutor owns ordered provider-path advancement after the
// exchange path has already been materialized.
//
// exchangeGraph only resolves the candidate paths; this helper decides which
// resolved path is tried next and keeps fallback truth local to one seam.
type orderedFallbackExecutor struct {
	Runner Runner
}

func (e orderedFallbackExecutor) Execute(ctx context.Context, in exchangeGraphInput, paths []exchangePathRecord) (TransportResponse, RoutableTarget, error) {
	for i, path := range paths {
		contract := NewExecutionContract(in.ClientDelivery).WithProviderDelivery(path.ProviderDelivery)
		response, runErr := e.Runner.Run(ctx, ExchangeInput{
			ExchangeID:       in.ExchangeID,
			ClientFamily:     in.ClientFamily,
			ClientDelivery:   contract.ClientDelivery,
			Request:          path.Request,
			Target:           path.Target,
			Contract:         contract,
			ProviderProtocol: path.ProtocolKind,
			ProviderDelivery: contract.ProviderDelivery,
		})
		if runErr == nil {
			return response, path.Target, nil
		}
		// Candidate-local Swobu failures are still pre-commit and may advance
		// to the next resolved path. Internal failures stay terminal so shared
		// orchestration bugs do not get hidden behind fallback.
		if i < len(paths)-1 && canAdvanceToNextPath(runErr) {
			continue
		}
		return TransportResponse{}, RoutableTarget{}, runErr
	}

	return TransportResponse{}, RoutableTarget{}, canonical.InternalError("no viable exchange path")
}

func canAdvanceToNextPath(err error) bool {
	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		return true
	}
	var swobuErr canonical.Error
	if errors.As(err, &swobuErr) {
		return swobuErr.Code != canonical.ErrorCodeInternal
	}
	return false
}
