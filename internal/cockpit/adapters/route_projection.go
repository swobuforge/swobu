package adapters

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/exchange"
)

func routesFromEndpoint(endpoint operatorclient.EndpointData) []readmodel.RouteReadModel {
	groups := map[string][]operatorclient.ProviderConfigData{}
	for _, target := range endpoint.ProviderConfigs {
		modelName := strings.TrimSpace(target.ModelID) // swobu:io-string source=boundary
		if modelName == "" {
			modelName = exchange.PublicModelIDSwobu
		}
		groups[modelName] = append(groups[modelName], target)
	}
	modelNames := make([]string, 0, len(groups))
	for modelName := range groups {
		modelNames = append(modelNames, modelName)
	}
	sort.Strings(modelNames)

	routes := make([]readmodel.RouteReadModel, 0, len(modelNames))
	selectedModel := selectedModelName(endpoint)
	for _, modelName := range modelNames {
		targets := targetsFromProviderConfigs(groups[modelName], endpoint.SelectedRef)
		routes = append(routes, readmodel.RouteReadModel{
			ID:        readmodel.RouteID(modelName),
			ModelName: modelName,
			State:     routeState(targets),
			PlanKind:  routePlanKind(targets),
			Default:   modelName == selectedModel,
			Enabled:   len(targets) > 0,
			Targets:   targets,
		})
	}
	return routes
}

func routeFromEndpoint(endpoint operatorclient.EndpointData, modelName string) (readmodel.RouteReadModel, error) {
	for _, route := range routesFromEndpoint(endpoint) {
		if route.ModelName == modelName {
			return route, nil
		}
	}
	return readmodel.RouteReadModel{}, errors.New("route could not be resolved after save")
}

func configRouteModel(config operatorclient.ProviderConfigData) string {
	modelName := strings.TrimSpace(config.ModelID) // swobu:io-string source=boundary
	if modelName == "" {
		return exchange.PublicModelIDSwobu
	}
	return modelName
}

func targetsFromProviderConfigs(configs []operatorclient.ProviderConfigData, selectedRef string) []readmodel.TargetReadModel {
	sort.Slice(configs, func(i, j int) bool {
		if configs[i].Ref == selectedRef && configs[j].Ref != selectedRef {
			return true
		}
		if configs[j].Ref == selectedRef && configs[i].Ref != selectedRef {
			return false
		}
		return configs[i].Ref < configs[j].Ref
	})
	targets := make([]readmodel.TargetReadModel, 0, len(configs))
	for i, config := range configs {
		targets = append(targets, targetFromProviderConfig(config, i+1))
	}
	return targets
}

func targetFromProviderConfig(config operatorclient.ProviderConfigData, rank int) readmodel.TargetReadModel {
	name := strings.TrimSpace(config.TargetAlias) // swobu:io-string source=boundary
	if name == "" {
		name = config.Ref
	}
	return readmodel.TargetReadModel{
		ID:            readmodel.TargetID(config.Ref),
		Name:          name,
		Provider:      config.ProviderSpec,
		Model:         config.ModelID,
		BaseURL:       config.BaseURL,
		CredentialRef: config.CredentialRef,
		Rank:          rank,
		Weight:        1,
	}
}

func providerConfigFromTargetRequest(request ports.SaveTargetRequest, targetID string) operatorclient.ProviderConfigData {
	modelID := strings.TrimSpace(request.Model) // swobu:io-string source=boundary
	if modelID == "" {
		modelID = strings.TrimSpace(string(request.RouteID)) // swobu:io-string source=boundary
	}
	return operatorclient.ProviderConfigData{
		Ref:           targetID,
		ProviderSpec:  strings.TrimSpace(request.Provider),      // swobu:io-string source=boundary
		BaseURL:       strings.TrimSpace(request.BaseURL),       // swobu:io-string source=boundary
		CredentialRef: strings.TrimSpace(request.CredentialRef), // swobu:io-string source=boundary
		ModelID:       modelID,
		TargetAlias:   strings.TrimSpace(request.Name), // swobu:io-string source=boundary
	}
}

func newProviderConfigRef(existing []operatorclient.ProviderConfigData) (string, error) {
	used := make(map[string]struct{}, len(existing))
	for _, config := range existing {
		used[config.Ref] = struct{}{}
	}
	var raw [8]byte
	for attempts := 0; attempts < 64; attempts++ {
		if _, err := rand.Read(raw[:]); err != nil {
			return "", err
		}
		ref := hex.EncodeToString(raw[:])
		if _, exists := used[ref]; exists {
			continue
		}
		return ref, nil
	}
	return "", errors.New("provider config ref could not be allocated")
}

func selectedModelName(endpoint operatorclient.EndpointData) string {
	for _, config := range endpoint.ProviderConfigs {
		if config.Ref == endpoint.SelectedRef {
			if model := strings.TrimSpace(config.ModelID); model != "" {
				return model
			}
			return exchange.PublicModelIDSwobu
		}
	}
	return ""
}

func routeState(targets []readmodel.TargetReadModel) readmodel.RouteState {
	if len(targets) == 0 {
		return readmodel.RouteIncomplete
	}
	return readmodel.RouteNormal
}

func routePlanKind(targets []readmodel.TargetReadModel) readmodel.RoutePlanKind {
	if len(targets) > 1 {
		return readmodel.RoutePlanRanked
	}
	return readmodel.RoutePlanSingle
}
