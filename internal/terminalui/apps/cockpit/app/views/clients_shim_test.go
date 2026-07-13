// Temporary test shims — TODO(v2-migration): replace callers with model-driven
// test assertions and delete this file.
package views

import (
	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type clientsSectionState struct {
	clientPickerOpen       bool
	setClientPickerOpen    func(bool)
	clientPickerCursor     int
	setClientPickerCursor  func(int)
	expandedActionID       string
	setExpandedActionID    func(string)
	payloadScrollOffset    int
	setPayloadScrollOffset func(int)
}

func toggleClientPicker(selectedFocusKey string, selectedCursor int, local clientsSectionState) []update.Action {
	nextOpen := !local.clientPickerOpen
	local.setClientPickerOpen(nextOpen)
	if !nextOpen {
		return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
	}
	if selectedCursor < 0 {
		selectedCursor = 0
	}
	local.setClientPickerCursor(selectedCursor)
	return []update.Action{
		interaction.FocusKeyAction{Key: selectedFocusKey},
		state.SetInteractionMode{Mode: state.InteractionModePickOne},
	}
}

func clientSummaryKeyHandler(selectedFocusKey string, selectedCursor int, local clientsSectionState) func(*retained.Context[state.Model], interaction.Event) (bool, []update.Action) {
	return func(_ *retained.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
		if ev.Kind != interaction.EventKey {
			return false, nil
		}
		switch ev.Key {
		case interaction.KeyEnter:
			return true, toggleClientPicker(selectedFocusKey, selectedCursor, local)
		case interaction.KeyEsc:
			if !local.clientPickerOpen {
				return false, nil
			}
			local.setClientPickerOpen(false)
			return true, []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
		default:
			return false, nil
		}
	}
}

func buildClientRow(profiles []clientprofile.Profile, summary string, selected clientprofile.Profile, local clientsSectionState) retained.ViewSpec[state.Model] {
	selectedCursor := clientPickerCursorForSelection(profiles, selected)
	selectedFocusKey := clientPickerFocusKey(selected)
	if selectedCursor >= 0 && selectedCursor < len(profiles) {
		selectedFocusKey = clientPickerFocusKey(profiles[selectedCursor])
	}
	summaryRow := toolkitviews.KeyScope(
		CoreNodeAsRetained(BuildClientsInteractiveSummaryNode(summary)),
		clientSummaryKeyHandler(selectedFocusKey, selectedCursor, local),
	)
	if !local.clientPickerOpen {
		return summaryRow
	}
	pickerRows := make([]retained.ViewSpec[state.Model], 0, len(profiles))
	for _, profile := range profiles {
		choice := profile
		pickerRows = append(pickerRows, retained.Named[state.Model](clientPickerFocusKey(choice),
			toolkitviews.KeyScope(
				CoreNodeAsRetained(buildClientPickerOptionNode(choice)),
				func(_ *retained.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
					if ev.Kind != interaction.EventKey {
						return false, nil
					}
					switch ev.Key {
					case interaction.KeyEnter, interaction.KeySpace:
						local.setClientPickerOpen(false)
						local.setExpandedActionID("")
						local.setPayloadScrollOffset(0)
						return true, []update.Action{
							state.SetSelectedClientID{ID: choice.Identity().ID},
							state.SetInteractionMode{Mode: state.InteractionModeNAV},
							interaction.FocusKeyAction{Key: "client"},
						}
					case interaction.KeyEsc:
						local.setClientPickerOpen(false)
						local.setPayloadScrollOffset(0)
						return true, []update.Action{
							state.SetInteractionMode{Mode: state.InteractionModeNAV},
							interaction.FocusKeyAction{Key: "client"},
						}
					default:
						return false, nil
					}
				},
			),
		))
	}
	optionStack := retained.VStack[state.Model](nil, pickerRows...)
	optionViewport := retained.Constrain[state.Model](
		retained.ScrollY[state.Model](optionStack, 0),
		retained.ConstrainSpec{GrowW: true, MaxW: ContentMaxWidth, MaxH: ListMaxHeight},
	)
	disclosure := EscClosableDisclosure(summaryRow, local.clientPickerOpen, func() []update.Action {
		local.setClientPickerOpen(false)
		local.setPayloadScrollOffset(0)
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeNAV},
			interaction.FocusKeyAction{Key: "client"},
		}
	}, optionViewport)
	return retained.View[state.Model](func(ctx *retained.Context[state.Model]) retained.RenderNode {
		child := retained.Materialize(ctx, disclosure)
		ph := func(ev interaction.Event) (bool, []update.Action) {
			if !local.clientPickerOpen || ev.Kind != interaction.EventKey || len(profiles) == 0 {
				return false, nil
			}
			next := local.clientPickerCursor
			if ev.Key == interaction.KeyUp {
				next--
			} else if ev.Key == interaction.KeyDown {
				next++
			} else {
				return false, nil
			}
			if next < 0 || next >= len(profiles) {
				return true, nil
			}
			local.setClientPickerCursor(next)
			return true, []update.Action{interaction.FocusKeyAction{Key: clientPickerFocusKey(profiles[next])}}
		}
		scope := toolkitviews.NewKeyScope(child, ph)
		scope.Fallback = func(ev interaction.Event) (bool, []update.Action) {
			if scoped, ok := child.(interaction.ScopedEventHandler); ok {
				return scoped.HandleScopedEvent(ev, nil)
			}
			return false, nil
		}
		return scope
	})
}

func buildClientPickerRow(profile clientprofile.Profile, local clientsSectionState) retained.ViewSpec[state.Model] {
	choice := profile
	return retained.Named[state.Model](clientPickerFocusKey(choice),
		toolkitviews.KeyScope(
			CoreNodeAsRetained(buildClientPickerOptionNode(choice)),
			func(_ *retained.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
				if ev.Kind != interaction.EventKey {
					return false, nil
				}
				switch ev.Key {
				case interaction.KeyEnter, interaction.KeySpace:
					local.setClientPickerOpen(false)
					local.setExpandedActionID("")
					local.setPayloadScrollOffset(0)
					return true, []update.Action{
						state.SetSelectedClientID{ID: choice.Identity().ID},
						state.SetInteractionMode{Mode: state.InteractionModeNAV},
						interaction.FocusKeyAction{Key: "client"},
					}
				case interaction.KeyEsc:
					local.setClientPickerOpen(false)
					local.setPayloadScrollOffset(0)
					return true, []update.Action{
						state.SetInteractionMode{Mode: state.InteractionModeNAV},
						interaction.FocusKeyAction{Key: "client"},
					}
				default:
					return false, nil
				}
			},
		),
	)
}
