package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/ports"
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
	Catalog(model state.Model) ([]ports.ProviderDeploymentRecord, string)
	ProbePending(model state.Model) bool
	CloseMode() string
}

type createDraftModelBinding struct{}

func (createDraftModelBinding) Snapshot(model state.Model) state.ProviderConfigSnapshot {
	return model.CreateDraftProviderConfig
}

func (createDraftModelBinding) SetSnapshot(next state.ProviderConfigSnapshot) []update.Action {
	actions := []update.Action{
		state.SetCreateDraftModelIDAction{ModelID: strings.TrimSpace(next.ModelID)}, // swobu:io-string source=boundary
		state.SetCreateDraftProviderProtocol{ProviderProtocol: strings.TrimSpace(next.ProviderProtocol)},
	}
	return actions
}

func (createDraftModelBinding) LoadCatalog(next state.ProviderConfigSnapshot) []update.Action {
	provider := strings.TrimSpace(next.ProviderSpec) // swobu:io-string source=boundary
	baseURL := effectiveDraftBaseURL(next)
	return []update.Action{
		state.LoadRoutingModelCatalogRequestedAction{
			Scope:            state.RoutingModelCatalogScopeCreateDraft,
			ProviderSpec:     provider,
			AuthHeader:       strings.TrimSpace(next.AuthHeader),       // swobu:io-string source=boundary
			ProviderProtocol: strings.TrimSpace(next.ProviderProtocol), // swobu:io-string source=boundary
			BaseURL:          baseURL,
			CredentialRef:    strings.TrimSpace(next.CredentialRef), // swobu:io-string source=boundary
		},
	}
}

func (createDraftModelBinding) Catalog(model state.Model) ([]ports.ProviderDeploymentRecord, string) {
	return model.CreateDraftModelDeployments, model.CreateDraftModelError
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
			AuthHeader:       strings.TrimSpace(next.AuthHeader),       // swobu:io-string source=boundary
			ProviderProtocol: strings.TrimSpace(next.ProviderProtocol), // swobu:io-string source=boundary
			BaseURL:          strings.TrimSpace(next.BaseURL),          // swobu:io-string source=boundary
			CredentialRef:    credentialRef,
		},
	}
}

func (addDraftModelBinding) Catalog(model state.Model) ([]ports.ProviderDeploymentRecord, string) {
	return model.AddModelDraftModelDeployments, model.AddModelDraftModelError
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

// swobu:lint ignore function-complexity because=draft-model picker keeps all branchy UI flow in one routing boundary.
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
	modelDeployments, modelErr := spec.Binding.Catalog(model)
	pending := spec.Binding.ProbePending(model)
	readiness := state.EvaluateModelSelectionGateState(state.ModelSelectionReadinessGateInput{
		ProviderSpec:      provider,
		BaseURL:           baseURL,
		CredentialRef:     cred,
		ModelCatalogError: modelErr,
	})

	modelSummary := selectors.EmptyOr(modelID, views.ValueRequired)
	if _, ok := spec.Binding.(addDraftModelBinding); ok && modelID == "" {
		modelSummary = views.ValueRequired
	}
	if spec.PickerOpen && modelID == "" {
		modelSummary = views.ValueRequired
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
	previewOptions := make([]modelPickerOption, 0, len(modelDeployments))
	for _, deployment := range modelDeployments {
		id := strings.TrimSpace(deployment.Name) // swobu:io-string source=domain
		if id == "" {
			continue
		}
		previewOptions = append(previewOptions, modelPickerOption{Key: id, Label: deploymentOptionLabel(deployment), Deployment: deployment})
	}
	previewFocusKey := modelPickerFirstFocusKey(previewOptions, spec.KeyPrefix)
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
		)
		if previewFocusKey != "" {
			actions = append(actions, interaction.FocusKeyAction{Key: previewFocusKey})
		}
		return actions
	}, nil, views.FocusAffordance("choose", false))
	if readiness.Blocked {
		if _, isCreate := spec.Binding.(createDraftModelBinding); isCreate && strings.TrimSpace(modelErr) != "" { // swobu:io-string source=boundary
			return manualCreateDraftModelEditor(ctx, draft, modelSummary, strings.TrimSpace(readiness.Message), spec) // swobu:io-string source=boundary
		}
		if message := trimRoutingInput(readiness.Message); message != "" {
			notes := views.DisclosureNoteRows(message)
			return retained.VStack(ctx, append([]retained.ViewSpec[state.Model]{views.RowStatic(views.RowModel, views.ValueBlocked)}, notes...)...)
		}
		return views.RowStatic(views.RowModel, views.ValueBlocked)
	}
	if provider == "" || !spec.PickerOpen {
		return modelRow
	}
	if strings.TrimSpace(modelErr) != "" { // swobu:io-string source=boundary
		if _, isCreate := spec.Binding.(createDraftModelBinding); isCreate {
			return manualCreateDraftModelEditor(ctx, draft, modelSummary, strings.TrimSpace(modelErr), spec) // swobu:io-string source=boundary
		}
		rows := []retained.ViewSpec[state.Model]{modelRow}
		rows = append(rows, views.DisclosureNoteRows(strings.TrimSpace(modelErr))...) // swobu:io-string source=boundary
		return retained.VStack(ctx, rows...)
	}
	if pending {
		rows := []retained.ViewSpec[state.Model]{modelRow}
		rows = append(rows, views.DisclosureNoteRows("loading models…")...)
		return retained.VStack(ctx, rows...)
	}
	if len(modelDeployments) == 0 {
		rows := []retained.ViewSpec[state.Model]{modelRow}
		rows = append(rows, views.DisclosureNoteRows("no models returned by provider catalog for current auth/provider configuration")...)
		return retained.VStack(ctx, rows...)
	}
	options := make([]modelPickerOption, 0, len(modelDeployments))
	for _, deployment := range modelDeployments {
		modelChoice := strings.TrimSpace(deployment.Name) // swobu:io-string source=boundary
		if modelChoice == "" {
			continue
		}
		deployment := deployment
		options = append(options, modelPickerOption{
			Key:        modelChoice,
			Label:      deploymentOptionLabel(deployment),
			Deployment: deployment,
			OnChoose: func() []update.Action {
				next := draft
				next.ModelID = modelChoice
				next.ProviderProtocol = deploymentSelectedProtocol(deployment, draft.ProviderSpec, draft.ProviderProtocol)
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
		strings.TrimSpace(draft.ModelID), // swobu:io-string source=boundary
		"deployment name",
		func(value string) []update.Action {
			next := draft
			next.ModelID = strings.TrimSpace(value) // swobu:io-string source=boundary
			actions := spec.Binding.SetSnapshot(next)
			return append(actions, state.SetInteractionMode{Mode: state.InteractionModeNAV})
		},
	)
	if strings.TrimSpace(readinessMessage) == "" { // swobu:io-string source=boundary
		return editor
	}
	rows := append([]retained.ViewSpec[state.Model]{editor}, views.DisclosureNoteRows(strings.TrimSpace(readinessMessage))...) // swobu:io-string source=boundary
	return retained.VStack(ctx, rows...)
}
