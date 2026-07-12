// Provider model row.
package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// providerModelChoiceRowSpec configures one provider-model row.
type providerModelChoiceRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerModelChoiceRow(spec providerModelChoiceRowSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildProviderModelChoiceRow(ctx, spec)
	})
}

func buildProviderModelChoiceRow(ctx *retained.Context[state.Model], spec providerModelChoiceRowSpec) retained.ViewSpec[state.Model] {
	if providerModelCatalogChoicesAvailable(spec) {
		return buildProviderModelCatalogChoiceRow(ctx, spec)
	}
	return retained.VStack(
		ctx,
		views.RowStatic(views.RowModel, views.ValueBlocked),
		views.RowStatic("", "-> model selection requires catalog-backed provider"),
	)
}

func providerModelCatalogChoicesAvailable(w providerModelChoiceRowSpec) bool {
	if w.CreateMode || w.ProviderConfig == nil {
		return false
	}
	spec := strings.TrimSpace(w.ProviderConfig.ProviderSpec) // swobu:io-string source=boundary
	return state.ProviderSupportsCatalog(spec)
}

func buildProviderModelCatalogChoiceRow(ctx *retained.Context[state.Model], w providerModelChoiceRowSpec) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	if w.ProviderConfig == nil {
		return views.RowStatic(views.RowModel, views.ValueBlocked)
	}
	readiness := state.EvaluateModelSelectionGateState(state.ModelSelectionReadinessGateInput{
		ProviderSpec:      strings.TrimSpace(w.ProviderConfig.ProviderSpec),  // swobu:io-string source=boundary
		BaseURL:           strings.TrimSpace(w.ProviderConfig.BaseURL),       // swobu:io-string source=boundary
		CredentialRef:     strings.TrimSpace(w.ProviderConfig.CredentialRef), // swobu:io-string source=boundary
		ModelCatalogError: model.AddModelDraftModelError,
	})
	if readiness.Blocked {
		blockedRows := []retained.ViewSpec[state.Model]{views.RowStatic(views.RowModel, views.ValueBlocked)}
		if message := strings.TrimSpace(readiness.Message); message != "" { // swobu:io-string source=boundary
			blockedRows = append(blockedRows, views.DisclosureNoteRows(message)...)
		}
		return retained.VStack(ctx, blockedRows...)
	}
	current := selectedModelID(model, w.ProviderConfig, w.CreateMode)
	if current == "" {
		current = views.ValueRequired
	}
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	closeMode := state.InteractionModeManageList
	if w.CreateMode {
		closeMode = state.InteractionModeNAV
	}
	previewOptions := make([]modelPickerOption, 0, len(model.AddModelDraftModelIDs))
	for _, modelID := range model.AddModelDraftModelIDs {
		id := strings.TrimSpace(modelID)
		if id == "" {
			continue
		}
		previewOptions = append(previewOptions, modelPickerOption{Key: id, Label: id})
	}
	previewFocusKey := modelPickerFirstFocusKey(previewOptions, "provider-model-option")
	parent := views.RowChoiceWithCancel(views.RowModel, current, func() []update.Action {
		nextOpen := !open
		setOpen(nextOpen)
		if nextOpen {
			views.ResetFilterablePickerState(setPicker)
		}
		mode := closeMode
		if nextOpen {
			mode = state.InteractionModePickOne
		}
		actions := []update.Action{state.SetInteractionMode{Mode: mode}}
		if nextOpen {
			pc := *w.ProviderConfig
			actions = append(actions, state.LoadRoutingModelCatalogRequestedAction{
				Scope:            state.RoutingModelCatalogScopeAddModelDraft,
				ProviderSpec:     strings.TrimSpace(pc.ProviderSpec),     // swobu:io-string source=boundary
				AuthHeader:       strings.TrimSpace(pc.AuthHeader),       // swobu:io-string source=boundary
				ProviderProtocol: strings.TrimSpace(pc.ProviderProtocol), // swobu:io-string source=boundary
				BaseURL:          strings.TrimSpace(pc.BaseURL),          // swobu:io-string source=boundary
				CredentialRef:    strings.TrimSpace(pc.CredentialRef),    // swobu:io-string source=boundary // swobu:io-string source=boundary
			})
			if previewFocusKey != "" {
				actions = append(actions, interaction.FocusKeyAction{Key: previewFocusKey})
			}
		}
		return actions
	}, func() []update.Action {
		if open {
			setOpen(false)
			return []update.Action{
				state.SetInteractionMode{Mode: closeMode},
				interaction.FocusKeyAction{Key: "model"},
			}
		}
		return nil
	})
	if !open {
		return parent
	}
	if !workspaceModelCatalogTupleMatches(model, w.ProviderConfig) {
		return toolkitviews.NewAnchoredDisclosure(parent, views.RowStatic("", "loading models…"))
	}
	options := make([]modelPickerOption, 0, len(model.AddModelDraftModelIDs))
	for _, modelID := range model.AddModelDraftModelIDs {
		selected := selectedModelID(ctx.Model(), w.ProviderConfig, w.CreateMode) == modelID
		choice := modelID
		options = append(options, modelPickerOption{
			Key:      choice,
			Label:    choice,
			Selected: selected,
			OnChoose: func() []update.Action {
				setOpen(false)
				actions := applyProviderModelSelection(choice, w.ProviderConfig, w.EndpointName, w.CreateMode)
				actions = append(actions, []update.Action{
					state.SetInteractionMode{Mode: closeMode},
					interaction.FocusKeyAction{Key: "model"},
				}...)
				return actions
			},
		})
	}
	if model.AddModelDraftModelError != "" || len(options) == 0 {
		rows := []retained.ViewSpec[state.Model]{parent}
		if model.AddModelDraftModelError != "" {
			rows = append(rows, views.DisclosureNoteRows(model.AddModelDraftModelError)...)
		} else {
			rows = append(rows, views.DisclosureNoteRows("no models available for current auth/provider configuration")...)
		}
		return retained.VStack(ctx, rows...)
	}
	return renderModelPickerDisclosure(ctx, modelPickerRenderSpec{
		Parent:    parent,
		Picker:    picker,
		SetPicker: setPicker,
		Options:   options,
		KeyPrefix: "provider-model-option",
		FocusKey:  "model",
		CloseDisclosure: func() []update.Action {
			setOpen(false)
			return []update.Action{
				state.SetInteractionMode{Mode: closeMode},
				interaction.FocusKeyAction{Key: "model"},
			}
		},
	})
}

func workspaceModelCatalogTupleMatches(model state.Model, providerConfig *state.ProviderConfigSnapshot) bool {
	if providerConfig == nil {
		return false
	}
	if model.AddModelDraftProviderSpec != providerConfig.ProviderSpec {
		return false
	}
	if model.AddModelDraftBaseURL != providerConfig.BaseURL {
		return false
	}
	if model.AddModelDraftAuthHeader != providerConfig.AuthHeader {
		return false
	}
	if model.AddModelDraftCredentialRef != providerConfig.CredentialRef {
		return false
	}
	if model.AddModelDraftProviderProtocol != providerConfig.ProviderProtocol {
		return false
	}
	return true
}

func applyProviderModelSelection(modelID string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	modelID = strings.TrimSpace(modelID) // swobu:io-string source=boundary
	if createMode {
		return []update.Action{state.SetCreateDraftModelIDAction{ModelID: modelID}}
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" { // swobu:io-string source=boundary
		return nil
	}
	next := *providerConfig
	next.ModelID = modelID
	return routingSaveProviderConfigActions(strings.TrimSpace(endpointName), next, "provider/model") // swobu:io-string source=boundary
}

func selectedModelID(model state.Model, providerConfig *state.ProviderConfigSnapshot, createMode bool) string {
	pc := selectedProvider(model, providerConfig, createMode)
	if pc == nil {
		return ""
	}
	return pc.ModelID
}

func selectedProvider(model state.Model, providerConfig *state.ProviderConfigSnapshot, createMode bool) *state.ProviderConfigSnapshot {
	if createMode {
		if providerConfig != nil {
			return providerConfig
		}
		return selectors.CreateDraftProviderConfig(model)
	}
	return providerConfig
}
