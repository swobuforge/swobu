package exchange

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func mapErrorToFailureClass(err error) routing.FailureClass {
	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		status := backendErr.StatusCode
		switch {
		case status == 404:
			return routing.FailureNotFound
		case status == 400:
			return routing.FailureBadRequest
		case status == 401 || status == 403:
			return routing.FailureAuth
		case status == 429:
			return routing.FailureRateLimited
		case status >= 500 && status < 600:
			return routing.FailureServerError
		default:
			return routing.FailureNetwork
		}
	}
	return routing.FailureUnknown
}

func endpointToWorkspaceRouting(e endpointintent.Endpoint) routing.WorkspaceRouting {
	routes := map[string]routing.Route{}
	for _, pc := range e.ProviderConfigs() {
		routeModelID := projectedRouteModel(pc)
		if routeModelID == "" {
			continue
		}
		mod := routes[routeModelID]
		mod.ModelName = routeModelID
		mod.Targets = append(mod.Targets, providerConfigToTarget(pc))
		routes[routeModelID] = mod
	}

	var defaultModel string
	if sel := e.SelectedProviderConfig(); sel.Ref().String() != "" {
		defaultModel = projectedRouteModel(sel)
	}

	return routing.WorkspaceRouting{
		WorkspaceSlug: e.Name().String(),
		DefaultModel:  defaultModel,
		Routes:        routes,
	}
}

// projectedRouteModel returns the client-visible route model name for request
// routing. Legacy configs without an explicit route model fall back to the
// provider-side model id.
func projectedRouteModel(pc endpointintent.ProviderConfig) string {
	routeModelID := strings.TrimSpace(pc.RouteModelID()) // swobu:io-string source=boundary
	if routeModelID != "" {
		return routeModelID
	}
	return strings.TrimSpace(pc.ModelID()) // swobu:io-string source=boundary
}

func providerConfigToTarget(pc endpointintent.ProviderConfig) routing.Target {
	return routing.Target{
		ID:            pc.Ref().String(),
		Provider:      pc.ProviderSpec().String(),
		CredentialRef: pc.CredentialRef(),
		Model:         pc.ModelID(),
		Protocol:      routing.ProtocolOverride(pc.ProviderProtocol()),
		Enabled:       true,
	}
}

func routeTargets(wr routing.WorkspaceRouting) []routing.Target {
	var all []routing.Target
	for _, r := range wr.Routes {
		all = append(all, r.Targets...)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].ID < all[j].ID
	})
	return all
}

func findProviderConfig(endpoint endpointintent.Endpoint, targetID string) endpointintent.ProviderConfig {
	for _, pc := range endpoint.ProviderConfigs() {
		if pc.Ref().String() == targetID {
			return pc
		}
	}
	return endpointintent.ProviderConfig{}
}

func buildPathRecord(
	ctx context.Context,
	exchangeID string,
	endpointName endpointintent.EndpointName,
	providerConfig endpointintent.ProviderConfig,
	clientDelivery delivery.Delivery,
	request canonical.CanonicalRequest,
) (exchangePathRecord, error) {
	routeProfile, ok := profile.ResolveRouteProfile(
		providerConfig.ProviderSpec().String(),
		providerConfig.BaseURL(),
		providerConfig.CredentialRef(),
	)
	if !ok {
		return exchangePathRecord{}, canonical.BadEndpoint("selected provider route is unsupported")
	}
	_ = routeProfile

	target := toRoutableTarget(providerConfig)
	providerDelivery := clientDelivery

	protocolKind, err := resolveProviderProtocolKind(
		providerConfig.ProviderSpec().String(),
		providerConfig.ProviderProtocol(),
		providerConfig.ProtocolKind(),
		request,
	)
	if err != nil {
		return exchangePathRecord{}, err
	}
	target.ProtocolKind = protocolKind

	modelID := providerConfig.ModelID()
	if modelID == "" {
		return exchangePathRecord{}, canonical.BadRequest("selected provider model is not configured")
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:         modelID,
		Instructions:  request.Instructions(),
		Items:         request.Items(),
		Tools:         request.Tools(),
		Turn:          request.Turn(),
		ToolPolicy:    request.ToolPolicy(),
		ToolCallBatch: request.ToolCallBatch(),
		Controls:      request.Controls(),
		OutputFormat:  request.OutputFormat(),
	})

	return exchangePathRecord{
		Request:          req,
		Target:           target,
		ProviderDelivery: providerDelivery,
		ProtocolKind:     protocolKind,
	}, nil
}

func toRoutableTarget(pc endpointintent.ProviderConfig) RoutableTarget {
	t := NewRoutableTarget(
		pc.Ref().String(),
		pc.ProviderSpec().String(),
		pc.BaseURL(),
		pc.CredentialRef(),
		pc.ProtocolKind(),
		"",
		pc.SelectedFrame(),
		pc.ProviderProtocol(),
	)
	t.AuthHeader = pc.AuthHeader()
	return t
}

func resolveProviderProtocolKind(
	targetSpec string,
	targetProtocol string,
	configured protocolkind.ProtocolKind,
	request canonical.CanonicalRequest,
) (protocolkind.ProtocolKind, error) {
	if !profile.SupportsSpec(targetSpec) {
		return "", canonical.BadEndpoint("provider id is unsupported")
	}
	if strings.TrimSpace(request.Model()) == "" { // swobu:io-string source=boundary
		return "", canonical.BadRequest("canonical request is required")
	}
	targetProtocol = strings.TrimSpace(targetProtocol) // swobu:io-string source=boundary
	autoProtocol := profile.ProviderProtocolAuto
	if targetProtocol == "" || targetProtocol == autoProtocol {
		if configured != "" {
			return configured, nil
		}
		return "", canonical.BadEndpoint("provider protocol must be concrete")
	}
	protocol, _, ok := profile.ProviderProtocolKindAndFrame(targetSpec, targetProtocol)
	if ok {
		if configured != "" && protocol != configured {
			return "", canonical.BadEndpoint("selected provider protocol is inconsistent with configured protocol kind")
		}
		return protocol, nil
	}
	return "", canonical.BadEndpoint("selected provider protocol is unsupported")
}

// exchangePathRecord is the bridge carrier from routing decision to Runner.Run.
type exchangePathRecord struct {
	Request          canonical.CanonicalRequest
	Target           RoutableTarget
	ProviderDelivery delivery.Delivery
	ProtocolKind     protocolkind.ProtocolKind
}
