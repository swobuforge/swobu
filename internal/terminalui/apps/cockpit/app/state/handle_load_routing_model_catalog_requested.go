package state

import (
	"strings"

	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func handleLoadRoutingModelCatalogRequested(model *Model, value LoadRoutingModelCatalogRequestedAction) []update.Effect {
	scope := strings.TrimSpace(value.Scope) // swobu:io-string source=boundary
	id := newRoutingProbeIdentity(scope, value.ProviderSpec, value.BaseURL, value.CredentialRef)
	providerProtocol := strings.TrimSpace(value.ProviderProtocol) // swobu:io-string source=boundary
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
	return []update.Effect{stateeffect.LoadRoutingModelCatalogEffect{
		Scope:            scope,
		ProviderSpec:     id.ProviderSpec,
		BaseURL:          id.BaseURL,
		CredentialRef:    id.CredentialRef,
		ProviderProtocol: providerProtocol,
	}}
}

func handleRoutingModelCatalogLoaded(model *Model, value stateeffect.RoutingModelCatalogLoaded) []update.Effect {
	scope := strings.TrimSpace(value.Scope) // swobu:io-string source=boundary
	if !matchesRoutingModelCatalogLoad(model, scope, strings.TrimSpace(value.ProviderSpec), strings.TrimSpace(value.BaseURL), strings.TrimSpace(value.CredentialRef), strings.TrimSpace(value.ProviderProtocol)) {
		return nil
	}
	if scope == RoutingModelCatalogScopeCreateDraft {
		model.CreateDraftModelIDs = append([]string(nil), value.ModelIDs...)
		model.CreateDraftModelError = strings.TrimSpace(value.Error) // swobu:io-string source=boundary
		model.CreateDraftModelProbePending = false
		resolvedVariant := strings.TrimSpace(value.ResolvedProviderProtocol) // swobu:io-string source=boundary
		model.CreateDraftModelTestProtocol = resolvedVariant
		model.CreateDraftModelTestPassed = model.CreateDraftModelError == ""
	} else if scope == RoutingModelCatalogScopeAddModelDraft {
		model.AddModelDraftModelIDs = append([]string(nil), value.ModelIDs...)
		model.AddModelDraftModelError = strings.TrimSpace(value.Error) // swobu:io-string source=boundary
		model.AddModelDraftModelProbePending = false
	}
	return nil
}
