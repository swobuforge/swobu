package adapters

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
)

// routesFromEndpoint projects daemon provider configs into Cockpit route rows.
//
// This is not a daemon route entity. A Cockpit route is the group of provider
// configs that share the same client-visible model name; mutations against a
// route rewrite or remove the configs in that projected group.
func routesFromEndpoint(endpoint operatorclient.EndpointData) []readmodel.RouteReadModel {
	groups := map[string][]operatorclient.ProviderConfigData{}
	for _, target := range endpoint.ProviderConfigs {
		modelName := projectedRouteModel(target)
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

// projectedRouteModel returns the client-visible model name used to group one
// provider config into a Cockpit route projection.
func projectedRouteModel(config operatorclient.ProviderConfigData) string {
	modelName := strings.TrimSpace(config.RouteModelID) // swobu:io-string source=boundary
	if modelName == "" {
		modelName = strings.TrimSpace(config.ModelID) // swobu:io-string source=boundary
	}
	if modelName == "" {
		return exchange.PublicModelIDSwobu
	}
	return modelName
}

func targetsFromProviderConfigs(configs []operatorclient.ProviderConfigData, selectedRef string) []readmodel.TargetReadModel {
	sort.Slice(configs, func(i, j int) bool {
		if targetRank(configs[i]) != targetRank(configs[j]) {
			return targetRank(configs[i]) < targetRank(configs[j])
		}
		if configs[i].Ref == selectedRef && configs[j].Ref != selectedRef {
			return true
		}
		if configs[j].Ref == selectedRef && configs[i].Ref != selectedRef {
			return false
		}
		return configs[i].Ref < configs[j].Ref
	})
	targets := make([]readmodel.TargetReadModel, 0, len(configs))
	for _, config := range configs {
		targets = append(targets, targetFromProviderConfig(config))
	}
	return targets
}

func targetFromProviderConfig(config operatorclient.ProviderConfigData) readmodel.TargetReadModel {
	name := strings.TrimSpace(config.TargetAlias) // swobu:io-string source=boundary
	if name == "" {
		name = config.Ref
	}
	return readmodel.TargetReadModel{
		ID:               readmodel.TargetID(config.Ref),
		Name:             name,
		Provider:         config.ProviderSpec,
		Model:            config.ModelID,
		ProviderProtocol: config.ProviderProtocol,
		BaseURL:          config.BaseURL,
		CredentialRef:    config.CredentialRef,
		Rank:             targetRank(config),
		Weight:           targetWeight(config),
	}
}

func providerConfigFromTargetRequest(request ports.SaveTargetRequest, targetID string) (operatorclient.ProviderConfigData, error) {
	if request.Rank < 1 {
		return operatorclient.ProviderConfigData{}, fmt.Errorf("target rank must be at least 1")
	}
	if request.Weight < 1 {
		return operatorclient.ProviderConfigData{}, fmt.Errorf("target weight must be at least 1")
	}
	modelID := strings.TrimSpace(request.Model) // swobu:io-string source=boundary
	if modelID == "" {
		modelID = strings.TrimSpace(string(request.RouteID)) // swobu:io-string source=boundary
	}
	protocol := strings.TrimSpace(request.ProviderProtocol)
	if protocol == "" {
		protocol = defaultProtocolForProvider(strings.TrimSpace(request.Provider))
	}
	if protocol != "" && !validProtocol(strings.TrimSpace(request.Provider), protocol) {
		return operatorclient.ProviderConfigData{}, fmt.Errorf("provider protocol %q is unsupported for provider %q", protocol, request.Provider)
	}

	return operatorclient.ProviderConfigData{
		Ref:              targetID,
		ProviderSpec:     strings.TrimSpace(request.Provider),      // swobu:io-string source=boundary
		BaseURL:          strings.TrimSpace(request.BaseURL),       // swobu:io-string source=boundary
		CredentialRef:    strings.TrimSpace(request.CredentialRef), // swobu:io-string source=boundary
		RouteModelID:     strings.TrimSpace(string(request.RouteID)), // swobu:io-string source=boundary
		ModelID:          modelID,
		ProviderProtocol: protocol,
		TargetAlias:      strings.TrimSpace(request.Name), // swobu:io-string source=boundary
		TargetRank:       request.Rank,
		TargetWeight:     request.Weight,
	}, nil
}

func validProtocol(spec, protocol string) bool {
	if protocol == "" {
		return true
	}
	return profile.SupportsProviderProtocolForSpec(spec, protocol)
}

func defaultProtocolForProvider(spec string) string {
	protocol, ok := profile.ResolveConcreteProtocolForAutoAtBoundary(spec)
	if !ok {
		return ""
	}
	return protocol
}

// targetMatchesRoute reports whether a provider config belongs to the given
// route and target identity pair. This is the canonical route-invariant
// check for all target mutations.
func targetMatchesRoute(config operatorclient.ProviderConfigData, targetID, routeID string) bool {
	return config.Ref == targetID && projectedRouteModel(config) == routeID
}

func targetRank(config operatorclient.ProviderConfigData) int {
	return positiveOrDefault(config.TargetRank, 1)
}

func targetWeight(config operatorclient.ProviderConfigData) int {
	return positiveOrDefault(config.TargetWeight, 1)
}

func positiveOrDefault(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
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
			return projectedRouteModel(config)
		}
	}
	return ""
}

// Cockpit does not model a separate zero-target route state; row copy handles
// that case directly in the routes surface.
func routeState(targets []readmodel.TargetReadModel) readmodel.RouteState {
	return readmodel.RouteNormal
}
