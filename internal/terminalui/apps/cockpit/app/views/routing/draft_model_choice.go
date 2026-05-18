package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// draftModelBinding is flow-agnostic authority for mutable draft model choice.
// Implementations adapt first-run create draft and workspace add-model draft.
type draftModelBinding interface {
	Snapshot(model state.Model) state.ProviderConfigSnapshot
	SetSnapshot(next state.ProviderConfigSnapshot) []update.Action
	LoadCatalog(next state.ProviderConfigSnapshot) []update.Action
	Catalog(model state.Model) ([]string, string)
	CloseMode() string
}

type createDraftModelBinding struct{}

func (createDraftModelBinding) Snapshot(model state.Model) state.ProviderConfigSnapshot {
	return model.CreateDraftProviderConfig
}

func (createDraftModelBinding) SetSnapshot(next state.ProviderConfigSnapshot) []update.Action {
	return []update.Action{
		state.SetCreateDraftModelIDAction{ModelID: strings.TrimSpace(next.ModelID)}, // swobu:io-string source=boundary
	}
}

func (createDraftModelBinding) LoadCatalog(next state.ProviderConfigSnapshot) []update.Action {
	provider := strings.TrimSpace(next.ProviderSpec) // swobu:io-string source=boundary
	return []update.Action{
		state.LoadRoutingModelCatalogRequestedAction{
			Scope:         state.RoutingModelCatalogScopeCreateDraft,
			ProviderSpec:  provider,
			BaseURL:       strings.TrimSpace(next.BaseURL),       // swobu:io-string source=boundary
			CredentialRef: strings.TrimSpace(next.CredentialRef), // swobu:io-string source=boundary
		},
	}
}

func (createDraftModelBinding) Catalog(model state.Model) ([]string, string) {
	return model.CreateDraftModelIDs, model.CreateDraftModelError
}

func (createDraftModelBinding) CloseMode() string { return state.InteractionModeNAV }

type addDraftModelBinding struct {
	model    state.Model
	draft    state.ProviderConfigSnapshot
	setDraft func(state.ProviderConfigSnapshot)
}

func (b addDraftModelBinding) Snapshot(_ state.Model) state.ProviderConfigSnapshot {
	return b.draft
}

func (b addDraftModelBinding) SetSnapshot(next state.ProviderConfigSnapshot) []update.Action {
	b.setDraft(next)
	return nil
}

func (b addDraftModelBinding) LoadCatalog(next state.ProviderConfigSnapshot) []update.Action {
	provider := strings.TrimSpace(next.ProviderSpec)       // swobu:io-string source=boundary
	credentialRef := strings.TrimSpace(next.CredentialRef) // swobu:io-string source=boundary
	credentialRef = effectiveAddModelCredentialRef(b.model, next)
	return []update.Action{
		state.LoadRoutingModelCatalogRequestedAction{
			Scope:         state.RoutingModelCatalogScopeAddModelDraft,
			ProviderSpec:  provider,
			BaseURL:       strings.TrimSpace(next.BaseURL), // swobu:io-string source=boundary
			CredentialRef: credentialRef,
		},
	}
}

func (addDraftModelBinding) Catalog(model state.Model) ([]string, string) {
	return model.AddModelDraftModelIDs, model.AddModelDraftModelError
}

func (addDraftModelBinding) CloseMode() string { return state.InteractionModeManageList }

type draftModelRowSpec struct {
	Binding        draftModelBinding
	PickerOpen     bool
	SetPickerOpen  func(bool)
	PickerState    views.FilterablePickerState
	SetPickerState func(views.FilterablePickerState)
	KeyPrefix      string
	FocusKey       string
}

func buildDraftModelChoiceRow(ctx *retained.Context[state.Model], spec draftModelRowSpec) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	draft := spec.Binding.Snapshot(model)
	provider := strings.TrimSpace(draft.ProviderSpec) // swobu:io-string source=boundary
	baseURL := strings.TrimSpace(draft.BaseURL)       // swobu:io-string source=boundary
	cred := strings.TrimSpace(draft.CredentialRef)    // swobu:io-string source=boundary
	if addBinding, ok := spec.Binding.(addDraftModelBinding); ok {
		cred = effectiveAddModelCredentialRef(addBinding.model, draft)
	}
	modelID := strings.TrimSpace(draft.ModelID) // swobu:io-string source=boundary
	modelIDs, modelErr := spec.Binding.Catalog(model)
	authFailed := state.ProviderModelCatalogAuthFailed(modelErr)

	modelSummary := selectors.EmptyOr(modelID, "not set")
	if _, ok := spec.Binding.(addDraftModelBinding); ok && modelID == "" {
		modelSummary = "not selected"
	}
	if spec.PickerOpen && modelID == "" {
		modelSummary = "choose a model"
	}
	blocked := state.ProviderModelCatalogLoadBlocked(provider, baseURL, cred) || authFailed
	if _, ok := spec.Binding.(createDraftModelBinding); ok {
		flow := state.EvaluateCreateDraftRouteSetup(draft)
		if flow.ModelState == state.RouteSetupSlotBlocked {
			blocked = true
		}
	}
	modelRow := views.RowChoiceWithHooks(views.RowModel, modelSummary, func() []update.Action {
		if provider == "" || blocked {
			return nil
		}
		spec.SetPickerOpen(true)
		views.ResetFilterablePickerState(spec.SetPickerState)
		actions := spec.Binding.LoadCatalog(draft)
		actions = append(actions,
			state.SetInteractionMode{Mode: state.InteractionModePickOne},
			interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey(spec.KeyPrefix, 0)},
		)
		return actions
	}, nil, views.FocusAffordance("choose", false))
	if blocked {
		if message, hasMessage := draftModelBlockedMessage(provider, baseURL, cred, modelErr, authFailed, isCreateDraftModelBinding(spec.Binding), draft); hasMessage {
			notes := views.DisclosureNoteRows(message)
			return retained.VStack(ctx, append([]retained.ViewSpec[state.Model]{views.RowStatic(views.RowModel, "blocked")}, notes...)...)
		}
		return views.RowStatic(views.RowModel, "blocked")
	}
	if provider == "" || !spec.PickerOpen {
		return modelRow
	}
	if strings.TrimSpace(modelErr) != "" || len(modelIDs) == 0 { // swobu:io-string source=boundary
		return backendURLEditorRow(ctx, views.RowModel, selectors.EmptyOr(strings.TrimSpace(draft.ModelID), "not set"), strings.TrimSpace(draft.ModelID), "model id", func(value string) []update.Action { // swobu:io-string source=boundary
			next := draft
			next.ModelID = strings.TrimSpace(value) // swobu:io-string source=boundary
			actions := spec.Binding.SetSnapshot(next)
			spec.SetPickerOpen(false)
			actions = append(actions,
				state.SetInteractionMode{Mode: spec.Binding.CloseMode()},
				interaction.FocusKeyAction{Key: spec.FocusKey},
			)
			return actions
		})
	}
	options := make([]modelPickerOption, 0, len(modelIDs))
	for _, choice := range modelIDs {
		modelChoice := choice
		options = append(options, modelPickerOption{
			Label: modelChoice,
			OnChoose: func() []update.Action {
				next := draft
				next.ModelID = modelChoice
				actions := spec.Binding.SetSnapshot(next)
				spec.SetPickerOpen(false)
				actions = append(actions,
					state.SetInteractionMode{Mode: spec.Binding.CloseMode()},
					interaction.FocusKeyAction{Key: spec.FocusKey},
				)
				return actions
			},
		})
	}
	return renderModelPickerDisclosure(ctx, modelPickerRenderSpec{
		Parent:    modelRow,
		Picker:    spec.PickerState,
		SetPicker: spec.SetPickerState,
		Options:   options,
		OnChooseRawID: func(rawID string) []update.Action {
			next := draft
			next.ModelID = strings.TrimSpace(rawID) // swobu:io-string source=boundary
			actions := spec.Binding.SetSnapshot(next)
			spec.SetPickerOpen(false)
			actions = append(actions,
				state.SetInteractionMode{Mode: spec.Binding.CloseMode()},
				interaction.FocusKeyAction{Key: spec.FocusKey},
			)
			return actions
		},
		KeyPrefix: spec.KeyPrefix,
		FocusKey:  spec.FocusKey,
		CloseDisclosure: func() []update.Action {
			spec.SetPickerOpen(false)
			return []update.Action{
				state.SetInteractionMode{Mode: spec.Binding.CloseMode()},
				interaction.FocusKeyAction{Key: spec.FocusKey},
			}
		},
	})
}

func isCreateDraftModelBinding(binding draftModelBinding) bool {
	_, ok := binding.(createDraftModelBinding)
	return ok
}

func draftModelBlockedMessage(provider, baseURL, cred, modelErr string, authFailed bool, createMode bool, draft state.ProviderConfigSnapshot) (string, bool) {
	if authFailed {
		return strings.TrimSpace(state.ProviderModelCatalogAuthFailureMessage(modelErr)), true // swobu:io-string source=boundary
	}
	if createMode {
		flow := state.EvaluateCreateDraftRouteSetup(draft)
		if flow.ModelBlocker != "" {
			return flow.ModelBlocker, true
		}
	}
	if message := strings.TrimSpace(state.ProviderModelCatalogBlockedMessage(provider, baseURL, cred)); message != "" { // swobu:io-string source=boundary
		return message, true
	}
	return "", false
}
