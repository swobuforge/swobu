package requestpath

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

type staticRoutingPolicy struct{}

func (staticRoutingPolicy) Decide(_ context.Context, endpoint endpointintent.Endpoint, intent ClientIntent) (RouteDecision, error) {
	catalog := buildEndpointModelCatalog(endpoint)
	selectedConfig, resolutionMode, err := resolveProviderConfigForRequest(endpoint, catalog, intent.RequestedModel)
	if err != nil {
		return RouteDecision{}, err
	}
	target, err := routableTargetFromProviderConfig(endpoint.Name(), selectedConfig)
	if err != nil {
		return RouteDecision{}, err
	}
	effectiveModel, err := effectiveModelIDForRequest(selectedConfig.ModelID())
	if err != nil {
		return RouteDecision{}, err
	}
	return RouteDecision{
		Target:         target,
		EffectiveModel: effectiveModel,
		ResolutionMode: resolutionMode,
		Reason:         resolutionMode,
	}, nil
}
