package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
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
		state.SetCreateDraftModelID{ModelID: strings.TrimSpace(next.ModelID)},
	}
}

func (createDraftModelBinding) LoadCatalog(next state.ProviderConfigSnapshot) []update.Action {
	provider := strings.TrimSpace(next.ProviderSpec)
	return []update.Action{
		state.LoadCreateDraftModelCatalogRequested{
			ProviderSpec:  provider,
			BaseURL:       strings.TrimSpace(next.BaseURL),
			CredentialRef: strings.TrimSpace(next.CredentialRef),
			ProtocolKind:  defaultProtocolKindForProvider(provider),
		},
	}
}

func (createDraftModelBinding) Catalog(model state.Model) ([]string, string) {
	return model.CreateDraftModelIDs, model.CreateDraftModelError
}

func (createDraftModelBinding) CloseMode() string { return state.InteractionModeNAV }

type addDraftModelBinding struct {
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

func (addDraftModelBinding) LoadCatalog(next state.ProviderConfigSnapshot) []update.Action {
	provider := strings.TrimSpace(next.ProviderSpec)
	return []update.Action{
		state.LoadAddModelDraftModelCatalogRequested{
			ProviderSpec:  provider,
			BaseURL:       strings.TrimSpace(next.BaseURL),
			CredentialRef: strings.TrimSpace(next.CredentialRef),
			ProtocolKind:  defaultProtocolKindForProvider(provider),
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

func buildDraftModelChoiceRow(ctx *view.Context[state.Model], spec draftModelRowSpec) view.ViewSpec[state.Model] {
	model := ctx.Model()
	draft := spec.Binding.Snapshot(model)
	provider := strings.TrimSpace(draft.ProviderSpec)
	baseURL := strings.TrimSpace(draft.BaseURL)
	cred := strings.TrimSpace(draft.CredentialRef)
	modelID := strings.TrimSpace(draft.ModelID)

	modelSummary := selectors.EmptyOr(modelID, "not set")
	if spec.PickerOpen && modelID == "" {
		modelSummary = "choose a model"
	}
	modelRow := views.RowChoiceWithHooks(views.RowModel, modelSummary, func() []update.Action {
		if provider == "" || providerModelCatalogLoadBlocked(provider, baseURL, cred) {
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

	if providerModelCatalogLoadBlocked(provider, baseURL, cred) {
		return toolkitviews.NewAnchoredDisclosure(modelRow, views.DisclosureNoteRows("set credential file before loading models")...)
	}
	if provider == "" || !spec.PickerOpen {
		return modelRow
	}

	modelIDs, modelErr := spec.Binding.Catalog(model)
	items := make([]views.FilterablePickerItem, 0, len(modelIDs))
	for _, choice := range modelIDs {
		modelChoice := choice
		items = append(items, views.FilterablePickerItem{
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
	if len(items) > 0 {
		return views.RenderFilterablePickerDisclosure(ctx, modelRow, spec.PickerState, spec.SetPickerState, items, views.FilterablePickerConfig{
			KeyPrefix:      spec.KeyPrefix,
			BuildOptionRow: views.ChoicePickerOptionRow(false),
			WindowSize:     6,
			FindLabel:      "find",
			OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: spec.FocusKey}} },
			OnCancel: func() []update.Action {
				spec.SetPickerOpen(false)
				return []update.Action{
					state.SetInteractionMode{Mode: spec.Binding.CloseMode()},
					interaction.FocusKeyAction{Key: spec.FocusKey},
				}
			},
		})
	}
	if strings.TrimSpace(modelErr) != "" {
		return toolkitviews.NewAnchoredDisclosure(modelRow, views.DisclosureNoteRows(modelErr)...)
	}
	return toolkitviews.NewAnchoredDisclosure(modelRow, views.RowStatic("", "loading models…"))
}
