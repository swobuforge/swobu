package requestpath

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

type staticRoutingPolicy struct{}

func (staticRoutingPolicy) Decide(_ context.Context, endpoint endpointintent.Endpoint, intent ClientIntent) (RouteDecision, error) {
	resolutionMode := validateRequestedPublicModel(intent.RequestedModel)
	selectedConfig := endpoint.SelectedProviderConfig()
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

func fallbackRouteDecisions(endpoint endpointintent.Endpoint, requestedModel string) ([]RouteDecision, error) {
	configs := endpoint.ProviderConfigs()
	selected := endpoint.SelectedProviderConfigRef()
	ordered := make([]endpointintent.ProviderConfig, 0, len(configs))
	for _, cfg := range configs {
		if cfg.Ref() == selected {
			ordered = append(ordered, cfg)
			break
		}
	}
	for _, cfg := range configs {
		if cfg.Ref() == selected {
			continue
		}
		ordered = append(ordered, cfg)
	}
	out := make([]RouteDecision, 0, len(ordered))
	for _, cfg := range ordered {
		target, err := routableTargetFromProviderConfig(endpoint.Name(), cfg)
		if err != nil {
			return nil, err
		}
		effectiveModel, err := effectiveModelIDForRequest(cfg.ModelID())
		if err != nil {
			return nil, err
		}
		resolutionMode := validateRequestedPublicModel(requestedModel)
		out = append(out, RouteDecision{
			Target:         target,
			EffectiveModel: effectiveModel,
			ResolutionMode: resolutionMode,
			Reason:         resolutionMode,
		})
	}
	return out, nil
}
