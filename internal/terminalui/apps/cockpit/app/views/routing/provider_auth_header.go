// Provider auth-header row.
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

type providerAuthHeaderRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerAuthHeaderRow(spec providerAuthHeaderRowSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildProviderAuthHeaderRow(ctx, spec)
	})
}

func buildProviderAuthHeaderRow(ctx *retained.Context[state.Model], spec providerAuthHeaderRowSpec) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	pc := selectedProvider(model, spec.ProviderConfig, spec.CreateMode)
	if pc == nil {
		return nil
	}
	providerSpec := strings.TrimSpace(pc.ProviderSpec) // swobu:io-string source=boundary
	options := stateModel.ProviderAuthHeaderOptions(providerSpec)
	if len(options) == 0 {
		return nil
	}
	current := strings.TrimSpace(pc.AuthHeader) // swobu:io-string source=boundary
	if current == "" {
		current = stateModel.ProviderDefaultAuthHeader(providerSpec)
	}

	open, setOpen := retained.UseState(ctx, func() bool { return false })
	commonOpen, setCommonOpen := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	closeMode := state.InteractionModeManageList
	if spec.CreateMode {
		closeMode = state.InteractionModeNAV
	}
	closeDisclosure := func() []update.Action {
		setOpen(false)
		setCommonOpen(false)
		return []update.Action{
			state.SetInteractionMode{Mode: closeMode},
			interaction.FocusKeyAction{Key: "auth-header"},
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
				return applyProviderAuthHeaderSelection(choice, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
			},
		})
	}

	parent := views.RowChoiceWithCancel("auth header", current, func() []update.Action {
		nextOpen := !open
		setOpen(nextOpen)
		if !nextOpen {
			setCommonOpen(false)
			return []update.Action{state.SetInteractionMode{Mode: closeMode}}
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
			if focusKey := views.FilterablePickerFirstFocusKey(items, views.FilterablePickerConfig{KeyPrefix: "auth-header-option"}); focusKey != "" {
				actions = append(actions, interaction.FocusKeyAction{Key: focusKey})
			}
			return actions
		}
		return []update.Action{
			state.SetInteractionMode{Mode: closeMode},
			interaction.FocusKeyAction{Key: "auth-header"},
		}
	}, func() []update.Action {
		if !commonOpen {
			return nil
		}
		setCommonOpen(false)
		return []update.Action{
			state.SetInteractionMode{Mode: closeMode},
			interaction.FocusKeyAction{Key: "auth-header"},
		}
	}, views.FocusAffordance("choose", false))
	if commonOpen {
		commonParent = views.RenderFilterablePickerDisclosure(ctx, commonParent, picker, setPicker, items, views.FilterablePickerConfig{
			KeyPrefix:      "auth-header-option",
			BuildOptionRow: views.ChoicePickerOptionRow(false),
			WindowSize:     6,
			FindLabel:      "find",
			ShowSelected:   true,
			OnNoMatchFocus: func() []update.Action {
				return []update.Action{interaction.FocusKeyAction{Key: "auth-header"}}
			},
			OnCancel: func() []update.Action {
				setCommonOpen(false)
				return []update.Action{
					state.SetInteractionMode{Mode: closeMode},
					interaction.FocusKeyAction{Key: "auth-header"},
				}
			},
		})
	}

	customRow := backendURLEditorRow(ctx, "custom", current, current, stateModel.ProviderDefaultAuthHeader(providerSpec), func(value string) []update.Action {
		setOpen(false)
		setCommonOpen(false)
		return applyProviderAuthHeaderSelection(value, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
	})
	return views.EscClosableDisclosure(parent, true, closeDisclosure, retained.Named[state.Model]("auth-header/common", commonParent), retained.Named[state.Model]("auth-header/custom", customRow))
}

func applyProviderAuthHeaderSelection(authHeader string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	authHeader = strings.TrimSpace(authHeader) // swobu:io-string source=boundary
	if createMode {
		actions := []update.Action{
			state.SetCreateDraftAuthHeaderAction{AuthHeader: authHeader},
			state.SetCreateDraftModelIDAction{ModelID: ""},
		}
		if providerConfig == nil {
			return actions
		}
		actions = append(actions, state.LoadRoutingModelCatalogRequestedAction{
			Scope:            state.RoutingModelCatalogScopeCreateDraft,
			ProviderSpec:     strings.TrimSpace(providerConfig.ProviderSpec), // swobu:io-string source=boundary
			AuthHeader:       authHeader,
			ProviderProtocol: strings.TrimSpace(providerConfig.ProviderProtocol), // swobu:io-string source=boundary
			BaseURL:          strings.TrimSpace(providerConfig.BaseURL),          // swobu:io-string source=boundary
			CredentialRef:    strings.TrimSpace(providerConfig.CredentialRef),    // swobu:io-string source=boundary
		})
		return actions
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" { // swobu:io-string source=boundary
		return nil
	}
	next := *providerConfig
	next.AuthHeader = authHeader
	return routingSaveProviderConfigActions(strings.TrimSpace(endpointName), next, "provider/auth-header") // swobu:io-string source=boundary
}
