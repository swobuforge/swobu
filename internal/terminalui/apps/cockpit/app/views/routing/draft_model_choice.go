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
	ProbePending(model state.Model) bool
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
	baseURL := effectiveDraftBaseURL(next)
	return []update.Action{
		state.LoadRoutingModelCatalogRequestedAction{
			Scope:            state.RoutingModelCatalogScopeCreateDraft,
			ProviderSpec:     provider,
			ProviderProtocol: strings.TrimSpace(next.ProviderProtocol), // swobu:io-string source=boundary
			BaseURL:          baseURL,
			CredentialRef:    strings.TrimSpace(next.CredentialRef), // swobu:io-string source=boundary
		},
	}
}

func (createDraftModelBinding) Catalog(model state.Model) ([]string, string) {
	return model.CreateDraftModelIDs, model.CreateDraftModelError
}

func (createDraftModelBinding) ProbePending(model state.Model) bool {
	return model.CreateDraftModelProbePending
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
			Scope:            state.RoutingModelCatalogScopeAddModelDraft,
			ProviderSpec:     provider,
			ProviderProtocol: strings.TrimSpace(next.ProviderProtocol), // swobu:io-string source=boundary
			BaseURL:          strings.TrimSpace(next.BaseURL),          // swobu:io-string source=boundary
			CredentialRef:    credentialRef,
		},
	}
}

func (addDraftModelBinding) Catalog(model state.Model) ([]string, string) {
	return model.AddModelDraftModelIDs, model.AddModelDraftModelError
}

func (addDraftModelBinding) ProbePending(model state.Model) bool {
	return model.AddModelDraftModelProbePending
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
	OnOpen         func()
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
	pending := spec.Binding.ProbePending(model)
	readiness := state.EvaluateModelSelectionGateState(state.ModelSelectionReadinessGateInput{
		ProviderSpec:      provider,
		BaseURL:           baseURL,
		CredentialRef:     cred,
		ModelCatalogError: modelErr,
	})

	modelSummary := selectors.EmptyOr(modelID, "not set")
	if _, ok := spec.Binding.(addDraftModelBinding); ok && modelID == "" {
		modelSummary = "not selected"
	}
	if spec.PickerOpen && modelID == "" {
		modelSummary = "choose a model"
	}
	if _, ok := spec.Binding.(createDraftModelBinding); ok {
		readiness = state.EvaluateModelSelectionGateState(state.ModelSelectionReadinessGateInput{
			ProviderSpec:      provider,
			BaseURL:           baseURL,
			CredentialRef:     cred,
			ModelCatalogError: modelErr,
			CreateDraft:       &draft,
		})
	}
	modelRow := views.RowChoiceWithHooks(views.RowModel, modelSummary, func() []update.Action {
		if provider == "" || readiness.Blocked {
			return nil
		}
		if spec.OnOpen != nil {
			spec.OnOpen()
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
	if readiness.Blocked {
		if _, isCreate := spec.Binding.(createDraftModelBinding); isCreate && strings.TrimSpace(modelErr) != "" {
			return manualCreateDraftModelEditor(ctx, draft, modelSummary, strings.TrimSpace(readiness.Message), spec)
		}
		if message := trimRoutingInput(readiness.Message); message != "" {
			notes := views.DisclosureNoteRows(message)
			return retained.VStack(ctx, append([]retained.ViewSpec[state.Model]{views.RowStatic(views.RowModel, "blocked")}, notes...)...)
		}
		return views.RowStatic(views.RowModel, "blocked")
	}
	if provider == "" || !spec.PickerOpen {
		return modelRow
	}
	if strings.TrimSpace(modelErr) != "" { // swobu:io-string source=boundary
		if _, isCreate := spec.Binding.(createDraftModelBinding); isCreate {
			return manualCreateDraftModelEditor(ctx, draft, modelSummary, strings.TrimSpace(modelErr), spec)
		}
		rows := []retained.ViewSpec[state.Model]{modelRow}
		rows = append(rows, views.DisclosureNoteRows(strings.TrimSpace(modelErr))...)
		return retained.VStack(ctx, rows...)
	}
	if pending {
		rows := []retained.ViewSpec[state.Model]{modelRow}
		rows = append(rows, views.DisclosureNoteRows("loading models…")...)
		return retained.VStack(ctx, rows...)
	}
	if len(modelIDs) == 0 {
		rows := []retained.ViewSpec[state.Model]{modelRow}
		rows = append(rows, views.DisclosureNoteRows("no models returned by provider catalog for current auth/provider configuration")...)
		return retained.VStack(ctx, rows...)
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

func manualCreateDraftModelEditor(
	ctx *retained.Context[state.Model],
	draft state.ProviderConfigSnapshot,
	modelSummary string,
	readinessMessage string,
	spec draftModelRowSpec,
) retained.ViewSpec[state.Model] {
	editor := backendURLEditorRow(
		ctx,
		views.RowModel,
		modelSummary,
		strings.TrimSpace(draft.ModelID),
		"provider model id",
		func(value string) []update.Action {
			next := draft
			next.ModelID = strings.TrimSpace(value) // swobu:io-string source=boundary
			actions := spec.Binding.SetSnapshot(next)
			return append(actions, state.SetInteractionMode{Mode: state.InteractionModeNAV})
		},
	)
	if strings.TrimSpace(readinessMessage) == "" {
		return editor
	}
	rows := append([]retained.ViewSpec[state.Model]{editor}, views.DisclosureNoteRows(strings.TrimSpace(readinessMessage))...)
	return retained.VStack(ctx, rows...)
}
