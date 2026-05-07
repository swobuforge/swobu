// Provider model row.
package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
)

// providerModelChoiceRowSpec configures one provider-model row.
type providerModelChoiceRowSpec struct {
	Catalog        *state.CatalogEntry
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
	var out retained.ViewSpec[state.Model]
	if providerModelCatalogChoicesAvailable(spec) {
		out = buildProviderModelCatalogChoiceRow(ctx, spec)
	} else {
		out = buildProviderModelManualEditorRow(ctx, spec)
	}
	return out
}

func providerModelCatalogChoicesAvailable(w providerModelChoiceRowSpec) bool {
	return !w.CreateMode && w.Catalog != nil && len(w.Catalog.ModelIDs) > 0
}

func buildProviderModelCatalogChoiceRow(ctx *retained.Context[state.Model], w providerModelChoiceRowSpec) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	current := modelSummary(model, w.Catalog)
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	closeMode := state.InteractionModeManageList
	if w.CreateMode {
		closeMode = state.InteractionModeNAV
	}
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
			actions = append(actions, interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("provider-model-option", 0)})
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
		if w.Catalog != nil && strings.TrimSpace(w.Catalog.Error) != "" {
			return toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows(w.Catalog.Error)...)
		}
		return parent
	}
	items := make([]views.FilterablePickerItem, 0, len(w.Catalog.ModelIDs))
	for _, modelID := range w.Catalog.ModelIDs {
		selected := selectedModelID(ctx.Model(), w.ProviderConfig, w.CreateMode) == modelID
		choice := modelID
		items = append(items, views.FilterablePickerItem{
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
	return views.RenderFilterablePickerDisclosure(ctx, parent, picker, setPicker, items, views.FilterablePickerConfig{
		KeyPrefix:      "provider-model-option",
		BuildOptionRow: views.ChoicePickerOptionRow(true),
		WindowSize:     6,
		FindLabel:      "find",
		ShowSelected:   true,
		OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: "model"}} },
		OnCancel: func() []update.Action {
			setOpen(false)
			return []update.Action{
				state.SetInteractionMode{Mode: closeMode},
				interaction.FocusKeyAction{Key: "model"},
			}
		},
	})
}

func buildProviderModelManualEditorRow(ctx *retained.Context[state.Model], w providerModelChoiceRowSpec) retained.ViewSpec[state.Model] {
	current := selectedModelID(ctx.Model(), w.ProviderConfig, w.CreateMode)
	summary := selectors.EmptyOr(current, "not set")
	parent := backendURLEditorRow(
		ctx,
		views.RowModel,
		summary,
		current,
		"model id",
		func(value string) []update.Action {
			return applyProviderModelSelection(value, w.ProviderConfig, w.EndpointName, w.CreateMode)
		},
	)
	if w.Catalog != nil && strings.TrimSpace(w.Catalog.Error) != "" {
		return toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows(w.Catalog.Error)...)
	}
	return parent
}

func applyProviderModelSelection(modelID string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	modelID = strings.TrimSpace(modelID)
	if createMode {
		return []update.Action{state.SetCreateDraftModelID{ModelID: modelID}}
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" {
		return nil
	}
	next := *providerConfig
	next.ModelID = modelID
	return []update.Action{
		state.RoutingSaveStartedAction{},
		state.SaveProviderConfigRequested{
			EndpointName:   strings.TrimSpace(endpointName),
			ProviderConfig: next,
		},
	}
}

func selectedModelID(model state.Model, providerConfig *state.ProviderConfigSnapshot, createMode bool) string {
	pc := selectedProvider(model, providerConfig, createMode)
	if pc == nil {
		return ""
	}
	return strings.TrimSpace(pc.ModelID)
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
