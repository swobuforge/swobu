// Clients section retained.
package views

import (
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type clientsSectionState struct {
	selectedClientID       string
	setSelectedClientID    func(string)
	clientPickerOpen       bool
	setClientPickerOpen    func(bool)
	clientPickerCursor     int
	setClientPickerCursor  func(int)
	expandedActionID       string
	setExpandedActionID    func(string)
	payloadScrollOffset    int
	setPayloadScrollOffset func(int)
}

func BuildClientsSection(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	if spec, ok := maybeStaticClientsSection(ctx, model); ok {
		return spec
	}
	baseURL := strings.TrimSpace(selectors.ClientBaseURL(model)) // swobu:io-string source=boundary
	profiles := clientprofile.Catalog()
	local := bindClientsSectionState(ctx)
	selected := selectedClientProfile(profiles, local.selectedClientID)
	summary := clientsSummaryLabel(selected)

	clientRow := buildClientRow(profiles, summary, local)
	actions := selectedClientActions(selected, baseURL)
	rows := []retained.ViewSpec[state.Model]{retained.Named[state.Model]("client", clientRow)}
	rows = append(rows, buildActionRows(model, actions, baseURL, selected, local)...)
	return retained.Named[state.Model](
		SectionClients,
		retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
			open, setOpen := retained.UseState(ctx, func() bool { return false })
			closeSection := func() []update.Action {
				if !open {
					return nil
				}
				setOpen(false)
				return []update.Action{
					state.SetInteractionMode{Mode: state.InteractionModeNAV},
					state.SetFocusedRowAffordance{Verb: "open"},
				}
			}
			titleRow := retained.Named[state.Model](
				"title",
				sectionToggleTitleRow(SectionClients, open, func() []update.Action {
					if open {
						return closeSection()
					}
					setOpen(true)
					return []update.Action{interaction.FocusKeyAction{Key: "client"}}
				}),
			)
			children := []retained.ViewSpec[state.Model]{titleRow}
			if open {
				children = append(children, rows...)
			} else {
				children = append(children, SummaryRow(summary))
			}
			return EscClosableDisclosure(retained.VStack(ctx, children...), open, closeSection)
		}),
	)
}

func maybeStaticClientsSection(ctx *retained.Context[state.Model], model state.Model) (retained.ViewSpec[state.Model], bool) {
	if model.CurrentEndpoint == "" {
		if model.InteractionMode == state.InteractionModeBusySave {
			return staticSectionSummary(ctx, SectionClients, "not set"), true
		}
		return NewCollapsibleSection(
			SectionClients,
			false,
			"open",
			SummaryRow("not set"),
		), true
	}
	if model.HeaderStatus == "saved" {
		return staticSectionSummary(ctx, SectionClients, "not set"), true
	}
	return nil, false
}

func bindClientsSectionState(ctx *retained.Context[state.Model]) clientsSectionState {
	selectedClientID, setSelectedClientID := retained.UseState(ctx, func() string { return "" })
	clientPickerOpen, setClientPickerOpen := retained.UseState(ctx, func() bool { return false })
	clientPickerCursor, setClientPickerCursor := retained.UseState(ctx, func() int { return 0 })
	expandedActionID, setExpandedActionID := retained.UseState(ctx, func() string { return "" })
	payloadScrollOffset, setPayloadScrollOffset := retained.UseState(ctx, func() int { return 0 })
	return clientsSectionState{
		selectedClientID:       selectedClientID,
		setSelectedClientID:    setSelectedClientID,
		clientPickerOpen:       clientPickerOpen,
		setClientPickerOpen:    setClientPickerOpen,
		clientPickerCursor:     clientPickerCursor,
		setClientPickerCursor:  setClientPickerCursor,
		expandedActionID:       expandedActionID,
		setExpandedActionID:    setExpandedActionID,
		payloadScrollOffset:    payloadScrollOffset,
		setPayloadScrollOffset: setPayloadScrollOffset,
	}
}

func selectedClientProfile(profiles []clientprofile.Profile, selectedClientID string) clientprofile.Profile {
	selected := clientprofile.FindByID(profiles, selectedClientID)
	if selected != nil {
		return selected
	}
	return clientprofile.FindByLabel(profiles, selectedClientID)
}

func clientsSummaryLabel(selected clientprofile.Profile) string {
	if selected == nil {
		return "not set"
	}
	return selected.Identity().Label
}

func selectedClientActions(selected clientprofile.Profile, baseURL string) []clientprofile.Action {
	if selected == nil {
		return nil
	}
	actions := selected.Actions(baseURL)
	if len(actions) == 0 {
		return nil
	}
	return actions
}

func buildClientRow(profiles []clientprofile.Profile, summary string, local clientsSectionState) retained.ViewSpec[state.Model] {
	selected := selectedClientProfile(profiles, local.selectedClientID)
	selectedCursor := clientPickerCursorForSelection(profiles, selected)
	selectedFocusKey := clientPickerFocusKey(selected)
	if selectedCursor >= 0 && selectedCursor < len(profiles) {
		selectedFocusKey = clientPickerFocusKey(profiles[selectedCursor])
	}
	clientRow := RowChoiceWithHooks("client", summary, func() []update.Action {
		return toggleClientPicker(selectedFocusKey, selectedCursor, local)
	}, func() []update.Action {
		return closeClientPicker(local)
	}, focusAffordance("choose", false))
	if !local.clientPickerOpen {
		return clientRow
	}
	options := buildClientPickerRows(profiles, local, selected)
	optionStack := retained.VStack[state.Model](nil, options...)
	optionViewport := retained.WithConstrain[state.Model](retained.ConstrainSpec{
		GrowW: true,
		MaxW:  ContentMaxWidth,
		MaxH:  ListMaxHeight,
	})(retained.WithScrollY[state.Model](0)(optionStack))
	disclosure := toolkitviews.NewAnchoredDisclosure(clientRow, optionViewport)
	return toolkitviews.KeyScope(disclosure, clientPickerKeyHandler(profiles, local))
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

func closeClientPicker(local clientsSectionState) []update.Action {
	if !local.clientPickerOpen {
		return nil
	}
	local.setClientPickerOpen(false)
	return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
}

func buildClientPickerRows(profiles []clientprofile.Profile, local clientsSectionState, selected clientprofile.Profile) []retained.ViewSpec[state.Model] {
	pickerRows := make([]retained.ViewSpec[state.Model], 0, len(profiles))
	for _, profile := range profiles {
		pickerRows = append(pickerRows, buildClientPickerRow(profile, local, selected))
	}
	return pickerRows
}

func buildClientPickerRow(profile clientprofile.Profile, local clientsSectionState, selected clientprofile.Profile) retained.ViewSpec[state.Model] {
	choice := profile
	isSelected := selected != nil && choice.Identity().ID == selected.Identity().ID
	return retained.Named[state.Model](clientPickerFocusKey(choice), toolkitviews.ListItemRowWithHooks[state.Model](
		toolkitviews.InsetLabel(choice.Identity().Label, 4),
		isSelected,
		false,
		true,
		func() []update.Action {
			local.setSelectedClientID(choice.Identity().ID)
			local.setClientPickerOpen(false)
			local.setExpandedActionID("")
			local.setPayloadScrollOffset(0)
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeNAV},
				interaction.FocusKeyAction{Key: "client"},
			}
		},
		func() []update.Action {
			local.setClientPickerOpen(false)
			local.setPayloadScrollOffset(0)
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeNAV},
				interaction.FocusKeyAction{Key: "client"},
			}
		},
		focusAffordance("select", false),
	))
}

func clientPickerCursorForSelection(profiles []clientprofile.Profile, selected clientprofile.Profile) int {
	if selected == nil {
		return 0
	}
	for i, profile := range profiles {
		if profile.Identity().ID == selected.Identity().ID {
			return i
		}
	}
	return 0
}

func clientPickerKeyHandler(profiles []clientprofile.Profile, local clientsSectionState) func(*retained.Context[state.Model], interaction.Event) (bool, []update.Action) {
	return func(_ *retained.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
		if !local.clientPickerOpen || ev.Kind != interaction.EventKey || len(profiles) == 0 {
			return false, nil
		}
		if ev.Key == interaction.KeyUp {
			return moveClientPickerCursor(profiles, local, -1)
		}
		if ev.Key == interaction.KeyDown {
			return moveClientPickerCursor(profiles, local, 1)
		}
		return false, nil
	}
}

func moveClientPickerCursor(profiles []clientprofile.Profile, local clientsSectionState, delta int) (bool, []update.Action) {
	next := local.clientPickerCursor + delta
	if next < 0 || next >= len(profiles) {
		return true, nil
	}
	local.setClientPickerCursor(next)
	return true, []update.Action{interaction.FocusKeyAction{Key: clientPickerFocusKey(profiles[next])}}
}

func buildActionRows(model state.Model, actions []clientprofile.Action, baseURL string, selected clientprofile.Profile, local clientsSectionState) []retained.ViewSpec[state.Model] {
	rows := make([]retained.ViewSpec[state.Model], 0, len(actions))
	seen := map[string]int{}
	for _, action := range actions {
		rowKey := actionRowFocusKey(action, seen)
		row := buildActionRow(model, action, baseURL, selected, local)
		rows = append(rows, retained.Named[state.Model](rowKey, row))
	}
	return rows
}

func actionRowFocusKey(action clientprofile.Action, seen map[string]int) string {
	base := strings.TrimSpace(action.ID) // swobu:io-string source=boundary
	if base == "" {
		base = action.RowLabel()
	}
	if base == "" {
		base = "action"
	}
	count := seen[base]
	seen[base] = count + 1
	if count == 0 {
		return "client-action/" + base
	}
	return "client-action/" + base + "/" + strconv.Itoa(count)
}

func buildActionRow(model state.Model, action clientprofile.Action, baseURL string, selected clientprofile.Profile, local clientsSectionState) retained.ViewSpec[state.Model] {
	const payloadMaxHeight = 8
	actionID := actionStableID(action)
	row := RowActionWithHooks(action.RowLabel(), action.ActionSummary(), action.ActionVerb(), func() []update.Action {
		return activateClientAction(model, action, actionID, baseURL, selected, local)
	}, func() []update.Action {
		if local.expandedActionID != actionID {
			return nil
		}
		local.setExpandedActionID("")
		local.setPayloadScrollOffset(0)
		return nil
	}, func() []update.Action {
		return focusAffordance(action.ActionVerb(), false)()
	})
	note := actionResultNote(model, action)
	if local.expandedActionID != actionID || !action.HasPayload() {
		if note != "" {
			return anchoredDisclosureWithScrollableDetails(row, payloadMaxHeight, 0, false, false, payloadTextRow("-> "+note))
		}
		return row
	}
	rows := contentRows(action.Content)
	if note != "" {
		rows = append(rows, payloadTextRow("-> "+note))
	}
	maxOffset := payloadMaxOffset(len(rows), payloadMaxHeight)
	disclosure := anchoredDisclosureWithScrollableDetails(
		row,
		payloadMaxHeight,
		local.payloadScrollOffset,
		local.payloadScrollOffset > 0,
		local.payloadScrollOffset < maxOffset,
		rows...,
	)
	return keyScopeForDisclosureScroll(disclosure, local, maxOffset)
}

func actionResultNote(model state.Model, action clientprofile.Action) string {
	if action.ActionVerb() == "run" {
		return model.ClientLaunchNote
	}
	return ""
}

func activateClientAction(model state.Model, action clientprofile.Action, actionID, baseURL string, selected clientprofile.Profile, local clientsSectionState) []update.Action {
	if action.HasPayload() {
		if local.expandedActionID != actionID {
			local.setExpandedActionID(actionID)
			local.setPayloadScrollOffset(0)
		}
	}
	if action.ActionVerb() != "run" || selected == nil {
		return nil
	}
	return []update.Action{
		state.ClientLaunchRequestedAction{
			BaseURL: baseURL,
			Preset:  selected.Identity().ID,
			ModelID: selectedClientRunModelID(model),
		},
	}
}

func selectedClientRunModelID(model state.Model) string {
	snapshot := selectors.CurrentEndpointSnapshot(model)
	if snapshot == nil {
		return ""
	}
	if strings.TrimSpace(snapshot.Name) == "" { // swobu:io-string source=boundary
		return ""
	}
	return exchange.PublicModelIDSwobu
}
