package modelcatalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

type modelCatalogProbeInput struct {
	ProviderConfigRef string
	ProviderSpec      string
	BaseURL           string
	CredentialRef     string
	ProtocolKind      protocolsurface.Kind
}

func probeRouteModels(ctx context.Context, providers ports.ProviderModelCatalog, input modelCatalogProbeInput) ([]string, error) {
	spec := strings.TrimSpace(strings.ToLower(input.ProviderSpec))
	baseURL := strings.TrimSpace(input.BaseURL)
	credentialRef := strings.TrimSpace(input.CredentialRef)
	routeProfile, ok := providercatalog.ResolveRouteProfile(spec, input.ProtocolKind, baseURL, credentialRef)
	if !ok {
		return nil, fmt.Errorf("selected provider route is unsupported")
	}
	models, err := providers.ListModels(ctx, ports.NewRoutableTarget(
		input.ProviderConfigRef,
		spec,
		baseURL,
		credentialRef,
		input.ProtocolKind,
		string(routeProfile.AuthKind),
		string(routeProfile.EndpointMode),
	))
	if err != nil {
		return nil, err
	}
	return ports.CloneModelIDs(models), nil
}
