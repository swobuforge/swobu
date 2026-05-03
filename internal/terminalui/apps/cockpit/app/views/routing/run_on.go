// Run-on row views for routing section.
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

// BuildRunOnCreateRow shows the run-on summary in create mode with disclosure when no provider is configured.
func BuildRunOnCreateRow(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	open, setOpen := view.UseState(ctx, func() bool { return false })
	picker, setPicker := view.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	var cancelFn func() []update.Action
	if open {
		cancelFn = func() []update.Action {
			setOpen(false)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
		}
	}
	parent := views.RowChoiceWithHooks(views.RowRunOn, createRunOnSummary(model), func() []update.Action {
		nextOpen := !open
		setOpen(nextOpen)
		if nextOpen {
			views.ResetFilterablePickerState(setPicker)
		}
		mode := state.InteractionModeNAV
		if nextOpen {
			mode = state.InteractionModePickOne
		}
		actions := []update.Action{state.SetInteractionMode{Mode: mode}}
		if nextOpen {
			actions = append(actions, interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("create-run-on-option", 0)})
		}
		return actions
	}, cancelFn, views.FocusAffordance("choose", false))
	out := parent
	if open {
		out = views.RenderFilterablePickerDisclosure(ctx, parent, picker, setPicker, createRunOnChoiceItems(model, func() []update.Action {
			setOpen(false)
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeNAV},
				interaction.FocusKeyAction{Key: "run_on"},
			}
		}), views.FilterablePickerConfig{
			KeyPrefix:      "create-run-on-option",
			BuildOptionRow: views.ChoicePickerOptionRow(false),
			WindowSize:     6,
			FindLabel:      "find",
			OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: "run_on"}} },
			OnCancel: func() []update.Action {
				setOpen(false)
				return []update.Action{
					state.SetInteractionMode{Mode: state.InteractionModeNAV},
					interaction.FocusKeyAction{Key: "run_on"},
				}
			},
		})
	}
	return out
}

func createRunOnChoiceItems(model state.Model, onCancel func() []update.Action) []views.FilterablePickerItem {
	pc := selectors.CreateDraftProviderConfig(model)
	if pc == nil {
		return createProviderSpecItems(model, onCancel)
	}
	providerSpec := strings.TrimSpace(pc.ProviderSpec)
	if providerSpec == "" {
		return nil
	}
	summary := providerConfigSummary(*pc)
	return []views.FilterablePickerItem{
		{
			Label:    summary,
			Selected: true,
			OnChoose: func() []update.Action {
				actions := []update.Action{state.SetCreateDraftProviderSpec{ProviderSpec: providerSpec}}
				if onCancel != nil {
					actions = append(actions, onCancel()...)
				}
				return actions
			},
		},
	}
}

func primaryModelChoiceItems(snapshot *state.EndpointSnapshot, onCancel func() []update.Action) []views.FilterablePickerItem {
	if snapshot == nil || len(snapshot.ProviderConfigs) == 0 {
		return nil
	}
	items := make([]views.FilterablePickerItem, 0, len(snapshot.ProviderConfigs))
	for _, pc := range snapshot.ProviderConfigs {
		providerRef := strings.TrimSpace(pc.Ref)
		label := modelTargetLabel(snapshot, pc)
		items = append(items, views.FilterablePickerItem{
			Label:    label,
			Selected: providerRef == snapshot.SelectedProviderConfigRef,
			OnChoose: func() []update.Action {
				return primaryModelChooseActions(snapshot, providerRef, onCancel)
			},
		})
	}
	return items
}

func primaryModelChooseActions(snapshot *state.EndpointSnapshot, providerRef string, onCancel func() []update.Action) []update.Action {
	closeActions := []update.Action(nil)
	if onCancel != nil {
		closeActions = onCancel()
	}
	if snapshot == nil {
		return closeActions
	}
	if strings.TrimSpace(providerRef) == strings.TrimSpace(snapshot.SelectedProviderConfigRef) {
		return closeActions
	}
	actions := []update.Action{
		state.RoutingSaveStartedAction{},
		state.SaveSelectedTargetRequested{
			EndpointName: strings.TrimSpace(snapshot.Name),
			ProviderRef:  strings.TrimSpace(providerRef),
		},
	}
	return append(actions, closeActions...)
}

// Back-compat helper for existing unit tests and call-sites that still use the
// historical naming.
func runOnProviderChooseActions(snapshot *state.EndpointSnapshot, providerRef string, onCancel func() []update.Action) []update.Action {
	return primaryModelChooseActions(snapshot, providerRef, onCancel)
}

// BuildRunOnWorkspaceRow shows the primary model chooser in workspace mode.
func BuildRunOnWorkspaceRow(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	snapshot := selectors.CurrentEndpointSnapshot(model)
	var out view.ViewSpec[state.Model]
	if snapshot == nil {
		out = views.RowStatic("primary", "not selected")
	} else {
		open, setOpen := view.UseState(ctx, func() bool { return false })
		picker, setPicker := view.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
		var cancelFn func() []update.Action
		if open {
			cancelFn = func() []update.Action {
				setOpen(false)
				return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
			}
		}
		parent := views.RowChoiceWithHooks(views.RowRunOn, selectedDefaultModelSummary(model, snapshot), func() []update.Action {
			nextOpen := !open
			setOpen(nextOpen)
			if nextOpen {
				views.ResetFilterablePickerState(setPicker)
			}
			mode := state.InteractionModeNAV
			if nextOpen {
				mode = state.InteractionModePickOne
			}
			actions := []update.Action{state.SetInteractionMode{Mode: mode}}
			if nextOpen {
				actions = append(actions, interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("primary-model-option", 0)})
			}
			return actions
		}, cancelFn, views.FocusAffordance("choose", false))
		if !open {
			out = parent
			if model.RoutingSaveError != "" {
				out = toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows(model.RoutingSaveError)...)
			}
		} else {
			out = views.RenderFilterablePickerDisclosure(ctx, parent, picker, setPicker, primaryModelChoiceItems(snapshot, func() []update.Action {
				setOpen(false)
				return []update.Action{
					state.SetInteractionMode{Mode: state.InteractionModeNAV},
					interaction.FocusKeyAction{Key: "run_on"},
				}
			}), views.FilterablePickerConfig{
				KeyPrefix:      "primary-model-option",
				BuildOptionRow: views.ChoicePickerOptionRow(true),
				WindowSize:     6,
				FindLabel:      "find",
				ShowSelected:   true,
				OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: "run_on"}} },
				OnCancel: func() []update.Action {
					setOpen(false)
					return []update.Action{
						state.SetInteractionMode{Mode: state.InteractionModeNAV},
						interaction.FocusKeyAction{Key: "run_on"},
					}
				},
			})
		}
	}
	return out
}

func modelTargetLabel(snapshot *state.EndpointSnapshot, pc state.ProviderConfigSnapshot) string {
	if selector := selectors.ProviderConfigRequestModelID(snapshot, pc.Ref); selector != "" {
		return selector
	}
	return providerConfigSummary(pc)
}
