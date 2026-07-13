package state

import (
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
)

func handleLoadRoutingModelCatalogRequested(model *Model, value LoadRoutingModelCatalogRequestedAction) []EffectOnce {
	scope := strings.TrimSpace(value.Scope)           // swobu:io-string source=boundary
	authHeader := strings.TrimSpace(value.AuthHeader) // swobu:io-string source=boundary
	if authHeader == "" {
		authHeader = stateModel.ProviderDefaultAuthHeader(value.ProviderSpec)
	}
	id := newRoutingProbeIdentity(scope, value.ProviderSpec, value.BaseURL, authHeader, value.CredentialRef)
	providerProtocol := strings.TrimSpace(value.ProviderProtocol) // swobu:io-string source=boundary
	if stateModel.ProviderModelCatalogLoadBlocked(id.ProviderSpec, id.BaseURL, id.CredentialRef) {
		return nil
	}
	if scope == RoutingModelCatalogScopeCreateDraft {
		if id.ProviderSpec == "" {
			clearCreateDraftModelCatalogProbe(model)
			return nil
		}
		primeCreateDraftModelCatalogProbe(model, id)
	} else if scope == RoutingModelCatalogScopeAddModelDraft {
		if id.ProviderSpec == "" {
			clearAddModelCatalogProbe(model)
			return nil
		}
		primeAddModelCatalogProbe(model, id, providerProtocol)
	} else {
		return nil
	}
	return []EffectOnce{stateeffect.LoadRoutingModelCatalogEffect{
		Scope:            scope,
		ProviderSpec:     id.ProviderSpec,
		BaseURL:          id.BaseURL,
		AuthHeader:       id.AuthHeader,
		CredentialRef:    id.CredentialRef,
		ProviderProtocol: providerProtocol,
	}}
}

func handleRoutingModelCatalogLoaded(model *Model, value stateeffect.RoutingModelCatalogLoaded) []EffectOnce {
	scope := strings.TrimSpace(value.Scope)                                                                                                                                                                                                             // swobu:io-string source=boundary
	if !matchesRoutingModelCatalogLoad(model, scope, strings.TrimSpace(value.ProviderSpec), strings.TrimSpace(value.BaseURL), strings.TrimSpace(value.AuthHeader), strings.TrimSpace(value.CredentialRef), strings.TrimSpace(value.ProviderProtocol)) { // swobu:io-string source=domain
		return nil
	}
	if scope == RoutingModelCatalogScopeCreateDraft {
		model.CreateDraftModelDeployments = profile.CloneProviderDeployments(value.Deployments)
		model.CreateDraftModelError = strings.TrimSpace(value.Error) // swobu:io-string source=boundary
		model.CreateDraftModelProbePending = false
		resolvedVariant := strings.TrimSpace(value.ResolvedProviderProtocol) // swobu:io-string source=boundary
		model.CreateDraftModelTestProtocol = resolvedVariant
		model.CreateDraftModelTestPassed = model.CreateDraftModelError == ""
	} else if scope == RoutingModelCatalogScopeAddModelDraft {
		model.AddModelDraftModelDeployments = profile.CloneProviderDeployments(value.Deployments)
		model.AddModelDraftModelError = strings.TrimSpace(value.Error) // swobu:io-string source=boundary
		model.AddModelDraftModelProbePending = false
	}
	return nil
}
