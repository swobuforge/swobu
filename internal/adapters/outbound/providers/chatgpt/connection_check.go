package chatgpt

import (
	"context"
	"net/http"

	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// CheckConnectionCatalog uses only the supplied scoped resolver. Unlike normal
// authoring discovery, a check never reads provider-owned storage or refreshes
// credentials after rejection: the observation must not mutate authentication.
func CheckConnectionCatalog(ctx context.Context, target provider.TargetSnapshot, client *http.Client, credentials providersruntime.CredentialProvider) (provider.TargetProbeResult, error) {
	e := NewExecutor(client, credentials)
	auth, err := e.resolveCredentialSnapshot(ctx, target.ProviderID(), target.CredentialRef)
	if err != nil {
		return provider.TargetProbeResult{}, err
	}
	models, _, err := e.fetchModelCatalog(ctx, target, auth)
	if err != nil {
		return provider.TargetProbeResult{}, err
	}
	options := make([]profile.ModelAuthoringOption, 0, len(models))
	for _, id := range models {
		options = append(options, profile.NewModelAuthoringOption(id, id, target.ProviderID(), "", target.ProviderID(), profile.ConcreteProviderProtocolsForSpec(target.ProviderID()), ""))
	}
	return provider.TargetProbeResult{Options: options}, nil
}
