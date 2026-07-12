// Add-model auth-header row.
package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func addModelAuthHeaderRow(ctx *retained.Context[state.Model], draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildAddModelAuthHeaderRow(ctx, draft, panel)
	})
}

func buildAddModelAuthHeaderRow(ctx *retained.Context[state.Model], draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	providerSpec := strings.TrimSpace(draft.ProviderSpec) // swobu:io-string source=boundary
	options := stateModel.ProviderAuthHeaderOptions(providerSpec)
	if len(options) == 0 {
		return nil
	}
	current := strings.TrimSpace(draft.AuthHeader) // swobu:io-string source=boundary
	if current == "" {
		current = stateModel.ProviderDefaultAuthHeader(providerSpec)
	}

	open, setOpen := retained.UseState(ctx, func() bool { return false })
	commonOpen, setCommonOpen := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	closeDisclosure := func() []update.Action {
		setOpen(false)
		setCommonOpen(false)
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeManageList},
			interaction.FocusKeyAction{Key: "add-model/auth-header"},
		}
	}

	items := make([]views.FilterablePickerItem, 0, len(options))
	for _, option := range options {
		choice := strings.TrimSpace(option) // swobu:io-string source=boundary
		if choice == "" {
			continue
		}
		items = append(items, views.FilterablePickerItem{
			Key:      choice,
			Label:    choice,
			Search:   choice,
			Selected: strings.EqualFold(choice, current),
			OnChoose: func() []update.Action {
				setCommonOpen(false)
				setOpen(false)
				return applyAddModelAuthHeaderSelection(model, providerSpec, choice, draft, panel)
			},
		})
	}

	parent := views.RowChoiceWithCancel("auth header", current, func() []update.Action {
		nextOpen := !open
		setOpen(nextOpen)
		if !nextOpen {
			setCommonOpen(false)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeManageList}}
		}
		views.ResetFilterablePickerState(setPicker)
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeManageList},
		}
	}, closeDisclosure)
	if !open {
		return parent
	}

	commonParent := views.RowChoiceWithHooks("common", current, func() []update.Action {
		nextOpen := !commonOpen
		setCommonOpen(nextOpen)
		if nextOpen {
			views.ResetFilterablePickerState(setPicker)
			actions := []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModePickOne},
			}
			if focusKey := views.FilterablePickerFirstFocusKey(items, views.FilterablePickerConfig{KeyPrefix: "add-model/auth-header-option"}); focusKey != "" {
				actions = append(actions, interaction.FocusKeyAction{Key: focusKey})
			}
			return actions
		}
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeManageList},
			interaction.FocusKeyAction{Key: "add-model/auth-header"},
		}
	}, func() []update.Action {
		if !commonOpen {
			return nil
		}
		setCommonOpen(false)
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeManageList},
			interaction.FocusKeyAction{Key: "add-model/auth-header"},
		}
	}, views.FocusAffordance("choose", false))
	if commonOpen {
		commonParent = views.RenderFilterablePickerDisclosure(ctx, commonParent, picker, setPicker, items, views.FilterablePickerConfig{
			KeyPrefix:      "add-model/auth-header-option",
			BuildOptionRow: views.ChoicePickerOptionRow(false),
			WindowSize:     6,
			FindLabel:      "find",
			ShowSelected:   true,
			OnNoMatchFocus: func() []update.Action {
				return []update.Action{interaction.FocusKeyAction{Key: "add-model/auth-header"}}
			},
			OnCancel: func() []update.Action {
				setCommonOpen(false)
				return []update.Action{
					state.SetInteractionMode{Mode: state.InteractionModeManageList},
					interaction.FocusKeyAction{Key: "add-model/auth-header"},
				}
			},
		})
	}

	customRow := backendURLEditorRow(ctx, "custom", current, current, stateModel.ProviderDefaultAuthHeader(providerSpec), func(value string) []update.Action {
		setOpen(false)
		setCommonOpen(false)
		return applyAddModelAuthHeaderSelection(model, providerSpec, value, draft, panel)
	})
	return views.EscClosableDisclosure(parent, true, closeDisclosure, retained.Named[state.Model]("add-model/auth-header/common", commonParent), retained.Named[state.Model]("add-model/auth-header/custom", customRow))
}

func applyAddModelAuthHeaderSelection(model state.Model, providerSpec string, authHeader string, draft state.ProviderConfigSnapshot, panel addModelPanelState) []update.Action {
	providerSpec = strings.TrimSpace(providerSpec) // swobu:io-string source=boundary
	if providerSpec == "" {
		return nil
	}
	authHeader = strings.TrimSpace(authHeader) // swobu:io-string source=boundary
	if strings.EqualFold(providerSpec, "openai_compatible") && authHeader == "" {
		authHeader = stateModel.ProviderDefaultAuthHeader(providerSpec)
	}
	next := draft
	next.AuthHeader = authHeader
	next.ModelID = ""
	panel.setDraft(next)
	panel.setModelPickerOpen(false)
	return []update.Action{
		state.LoadRoutingModelCatalogRequestedAction{
			Scope:            state.RoutingModelCatalogScopeAddModelDraft,
			ProviderSpec:     providerSpec,
			AuthHeader:       authHeader,
			ProviderProtocol: strings.TrimSpace(next.ProviderProtocol), // swobu:io-string source=boundary
			BaseURL:          strings.TrimSpace(next.BaseURL),          // swobu:io-string source=boundary
			CredentialRef:    strings.TrimSpace(effectiveAddModelCredentialRef(model, next)),
		},
	}
}
