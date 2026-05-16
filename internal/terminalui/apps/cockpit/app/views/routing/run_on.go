// Run-on row views for routing section.
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

// BuildRunOnCreateRow shows the run-on summary in create mode with disclosure when no provider is configured.
func BuildRunOnCreateRow(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
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
	providerSpec := strings.TrimSpace(pc.ProviderSpec) // trimlowerlint:allow boundary canonicalization
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
		providerRef := strings.TrimSpace(pc.Ref) // trimlowerlint:allow boundary canonicalization
		label := modelTargetLabel(pc)
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
	if strings.TrimSpace(providerRef) == strings.TrimSpace(snapshot.SelectedProviderConfigRef) { // trimlowerlint:allow boundary canonicalization
		return closeActions
	}
	actions := routingSaveSelectedTargetActions(strings.TrimSpace(snapshot.Name), strings.TrimSpace(providerRef), "run_on") // trimlowerlint:allow boundary canonicalization
	return append(actions, closeActions...)
}

// BuildRunOnWorkspaceRow shows the primary model chooser in workspace mode.
func BuildRunOnWorkspaceRow(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	snapshot := selectors.CurrentEndpointSnapshot(model)
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	var out retained.ViewSpec[state.Model]
	if snapshot == nil {
		out = views.RowStatic("primary", "not selected")
	} else {
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
			if message := views.ScopedError(model, "routing", "run_on"); message != "" {
				out = toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows(message)...)
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

func modelTargetLabel(pc state.ProviderConfigSnapshot) string {
	return providerConfigSummary(pc)
}
