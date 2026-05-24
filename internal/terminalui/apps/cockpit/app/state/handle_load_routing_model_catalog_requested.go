package state

import (
	"strings"

	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func handleLoadRoutingModelCatalogRequested(model *Model, value LoadRoutingModelCatalogRequestedAction) []update.Effect {
	scope := strings.TrimSpace(value.Scope)                       // swobu:io-string source=boundary
	spec := strings.TrimSpace(value.ProviderSpec)                 // swobu:io-string source=boundary
	baseURL := strings.TrimSpace(value.BaseURL)                   // swobu:io-string source=boundary
	credentialRef := strings.TrimSpace(value.CredentialRef)       // swobu:io-string source=boundary
	providerProtocol := strings.TrimSpace(value.ProviderProtocol) // swobu:io-string source=boundary
	if scope == RoutingModelCatalogScopeCreateDraft {
		if spec == "" {
			model.CreateDraftModelIDs = nil
			model.CreateDraftModelError = ""
			model.CreateDraftModelProbePending = false
			model.CreateDraftModelProviderSpec = ""
			model.CreateDraftModelBaseURL = ""
			model.CreateDraftModelCredentialRef = ""
			model.CreateDraftModelTestProtocol = ""
			model.CreateDraftModelTestPassed = false
			return nil
		}
		model.CreateDraftModelProviderSpec = spec
		model.CreateDraftModelBaseURL = baseURL
		model.CreateDraftModelCredentialRef = credentialRef
		model.CreateDraftProviderConfig.ProviderProtocol = providerProtocol
		model.CreateDraftModelIDs = nil
		model.CreateDraftModelError = ""
		model.CreateDraftModelProbePending = true
		model.CreateDraftModelTestProtocol = ""
		model.CreateDraftModelTestPassed = false
	} else if scope == RoutingModelCatalogScopeAddModelDraft {
		if spec == "" {
			model.AddModelDraftModelIDs = nil
			model.AddModelDraftModelError = ""
			model.AddModelDraftModelProbePending = false
			model.AddModelDraftProviderSpec = ""
			model.AddModelDraftProviderProtocol = ""
			model.AddModelDraftBaseURL = ""
			model.AddModelDraftCredentialRef = ""
			return nil
		}
		model.AddModelDraftProviderSpec = spec
		model.AddModelDraftProviderProtocol = providerProtocol
		model.AddModelDraftBaseURL = baseURL
		model.AddModelDraftCredentialRef = credentialRef
		model.AddModelDraftProviderSpec = spec
		model.AddModelDraftModelIDs = nil
		model.AddModelDraftModelError = ""
		model.AddModelDraftModelProbePending = true
	} else {
		return nil
	}
	return []update.Effect{stateeffect.LoadRoutingModelCatalogEffect{Scope: scope, ProviderSpec: spec, BaseURL: baseURL, CredentialRef: credentialRef, ProviderProtocol: providerProtocol}}
}

func handleProbeCreateDraftModelRequested(model *Model, value ProbeCreateDraftModelRequestedAction) []update.Effect {
	spec := strings.TrimSpace(value.ProviderSpec)                 // swobu:io-string source=boundary
	baseURL := strings.TrimSpace(value.BaseURL)                   // swobu:io-string source=boundary
	credentialRef := strings.TrimSpace(value.CredentialRef)       // swobu:io-string source=boundary
	modelID := strings.TrimSpace(value.ModelID)                   // swobu:io-string source=boundary
	providerProtocol := strings.TrimSpace(value.ProviderProtocol) // swobu:io-string source=boundary
	if spec == "" || modelID == "" {
		model.CreateDraftModelProbePending = false
		model.CreateDraftModelTestPassed = false
		model.CreateDraftModelError = ""
		return nil
	}
	model.CreateDraftModelProbePending = true
	model.CreateDraftModelError = ""
	model.CreateDraftModelTestProtocol = ""
	model.CreateDraftModelTestPassed = false
	return []update.Effect{stateeffect.ProbeCreateDraftModelEffect{ProviderSpec: spec, BaseURL: baseURL, CredentialRef: credentialRef, ModelID: modelID, ProviderProtocol: providerProtocol}}
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
		if resolvedVariant != "" {
			model.CreateDraftProviderConfig.ProviderProtocol = resolvedVariant
		}
		model.CreateDraftModelTestPassed = model.CreateDraftModelError == ""
	} else if scope == RoutingModelCatalogScopeAddModelDraft {
		model.AddModelDraftModelIDs = append([]string(nil), value.ModelIDs...)
		model.AddModelDraftModelError = strings.TrimSpace(value.Error) // swobu:io-string source=boundary
		model.AddModelDraftModelProbePending = false
	}
	return nil
}

func handleCreateDraftModelProbeCompleted(model *Model, value stateeffect.CreateDraftModelProbeCompletedAction) []update.Effect {
	if strings.TrimSpace(value.ProviderSpec) != strings.TrimSpace(model.CreateDraftProviderConfig.ProviderSpec) || strings.TrimSpace(value.BaseURL) != strings.TrimSpace(model.CreateDraftProviderConfig.BaseURL) || strings.TrimSpace(value.CredentialRef) != strings.TrimSpace(model.CreateDraftProviderConfig.CredentialRef) || strings.TrimSpace(value.ModelID) != strings.TrimSpace(model.CreateDraftProviderConfig.ModelID) {
		return nil
	}
	model.CreateDraftModelProbePending = false
	model.CreateDraftModelError = strings.TrimSpace(value.Error)         // swobu:io-string source=boundary
	resolvedVariant := strings.TrimSpace(value.ResolvedProviderProtocol) // swobu:io-string source=boundary
	model.CreateDraftModelTestProtocol = resolvedVariant
	if resolvedVariant != "" {
		model.CreateDraftProviderConfig.ProviderProtocol = resolvedVariant
	}
	model.CreateDraftModelTestPassed = model.CreateDraftModelError == ""
	return nil
}
