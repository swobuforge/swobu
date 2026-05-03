// Clients section view.
package views

import (
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
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

func BuildClientsSection(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	if spec, ok := maybeStaticClientsSection(ctx, model); ok {
		return spec
	}
	baseURL := strings.TrimSpace(selectors.ClientBaseURL(model))
	profiles := clientprofile.Catalog()
	local := bindClientsSectionState(ctx)
	selected := selectedClientProfile(profiles, local.selectedClientID)
	summary := clientsSummaryLabel(selected)

	clientRow := buildClientRow(profiles, summary, local)
	actions := selectedClientActions(selected, baseURL)
	rows := []view.ViewSpec[state.Model]{
		view.Named[state.Model]("client", clientRow),
	}
	rows = append(rows, buildActionRows(model, actions, baseURL, selected, local)...)

	return NewCollapsibleSection(
		SectionClients,
		false,
		"open",
		SummaryRow(summary),
		rows...,
	)
}

func maybeStaticClientsSection(ctx *view.Context[state.Model], model state.Model) (view.ViewSpec[state.Model], bool) {
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

func bindClientsSectionState(ctx *view.Context[state.Model]) clientsSectionState {
	selectedClientID, setSelectedClientID := view.UseState(ctx, func() string { return "" })
	clientPickerOpen, setClientPickerOpen := view.UseState(ctx, func() bool { return false })
	clientPickerCursor, setClientPickerCursor := view.UseState(ctx, func() int { return 0 })
	expandedActionID, setExpandedActionID := view.UseState(ctx, func() string { return "" })
	payloadScrollOffset, setPayloadScrollOffset := view.UseState(ctx, func() int { return 0 })
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
		return []clientprofile.Action{{ID: "setup", Label: "setup", Verb: "view"}}
	}
	actions := selected.Actions(baseURL)
	configured := make([]clientprofile.Action, 0, len(actions))
	for _, action := range actions {
		if action.IsConfigured() {
			configured = append(configured, action)
		}
	}
	if len(configured) == 0 {
		return []clientprofile.Action{{ID: "setup", Label: "setup", Verb: "view"}}
	}
	return configured
}

func buildClientRow(profiles []clientprofile.Profile, summary string, local clientsSectionState) view.ViewSpec[state.Model] {
	clientRow := RowChoiceWithHooks("client", summary, func() []update.Action {
		return toggleClientPicker(local)
	}, func() []update.Action {
		return closeClientPicker(local)
	}, focusAffordance("choose", false))
	if !local.clientPickerOpen {
		return clientRow
	}
	options := buildClientPickerRows(profiles, local)
	optionStack := view.VStack[state.Model](nil, options...)
	optionViewport := view.WithConstrain[state.Model](view.ConstrainSpec{
		GrowW: true,
		MaxW:  ContentMaxWidth,
		MaxH:  ListMaxHeight,
	})(view.WithScrollY[state.Model](0)(optionStack))
	disclosure := toolkitviews.NewAnchoredDisclosure(clientRow, optionViewport)
	return toolkitviews.KeyScope(disclosure, clientPickerKeyHandler(profiles, local))
}

func toggleClientPicker(local clientsSectionState) []update.Action {
	nextOpen := !local.clientPickerOpen
	local.setClientPickerOpen(nextOpen)
	if !nextOpen {
		return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
	}
	local.setClientPickerCursor(0)
	return []update.Action{
		interaction.FocusKeyAction{Key: clientPickerFocusKey(0)},
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

func buildClientPickerRows(profiles []clientprofile.Profile, local clientsSectionState) []view.ViewSpec[state.Model] {
	pickerRows := make([]view.ViewSpec[state.Model], 0, len(profiles))
	for i, profile := range profiles {
		pickerRows = append(pickerRows, buildClientPickerRow(profile, i, local))
	}
	return pickerRows
}

func buildClientPickerRow(profile clientprofile.Profile, index int, local clientsSectionState) view.ViewSpec[state.Model] {
	choice := profile
	return view.Named[state.Model](clientPickerFocusKey(index), toolkitviews.ListItemRowWithHooks[state.Model](
		toolkitviews.InsetLabel(choice.Identity().Label, 4),
		false,
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

func clientPickerKeyHandler(profiles []clientprofile.Profile, local clientsSectionState) func(*view.Context[state.Model], interaction.Event) (bool, []update.Action) {
	return func(_ *view.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
		if !local.clientPickerOpen || ev.Kind != interaction.EventKey || len(profiles) == 0 {
			return false, nil
		}
		if ev.Key == interaction.KeyUp {
			return moveClientPickerCursor(local, -1, len(profiles))
		}
		if ev.Key == interaction.KeyDown {
			return moveClientPickerCursor(local, 1, len(profiles))
		}
		return false, nil
	}
}

func moveClientPickerCursor(local clientsSectionState, delta, optionCount int) (bool, []update.Action) {
	next := local.clientPickerCursor + delta
	if next < 0 || next >= optionCount {
		return true, nil
	}
	local.setClientPickerCursor(next)
	return true, []update.Action{interaction.FocusKeyAction{Key: clientPickerFocusKey(next)}}
}

func buildActionRows(model state.Model, actions []clientprofile.Action, baseURL string, selected clientprofile.Profile, local clientsSectionState) []view.ViewSpec[state.Model] {
	rows := make([]view.ViewSpec[state.Model], 0, len(actions))
	seen := map[string]int{}
	for index, action := range actions {
		rowKey := actionRowFocusKey(action, index, seen)
		row := buildActionRow(model, action, baseURL, selected, local)
		rows = append(rows, view.Named[state.Model](rowKey, row))
	}
	return rows
}

func actionRowFocusKey(action clientprofile.Action, index int, seen map[string]int) string {
	base := action.RowLabel()
	if base == "" {
		base = "action"
	}
	count := seen[base]
	seen[base] = count + 1
	if count == 0 {
		return base
	}
	return base + "/" + strconv.Itoa(index)
}

func buildActionRow(model state.Model, action clientprofile.Action, baseURL string, selected clientprofile.Profile, local clientsSectionState) view.ViewSpec[state.Model] {
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
		return focusAffordance(action.EffectiveFocusVerb(), false)()
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
	switch action.ActionVerb() {
	case "run":
		return model.ClientLaunchNote
	case "copy":
		return model.ClientCopyNote
	default:
		return ""
	}
}

func activateClientAction(model state.Model, action clientprofile.Action, actionID, baseURL string, selected clientprofile.Profile, local clientsSectionState) []update.Action {
	actions := make([]update.Action, 0, 2)
	if action.HasPayload() {
		if local.expandedActionID != actionID {
			local.setExpandedActionID(actionID)
			local.setPayloadScrollOffset(0)
		}
	}
	switch action.ActionVerb() {
	case "run":
		if selected != nil {
			actions = append(actions, state.ClientLaunchRequested{
				BaseURL: baseURL,
				Preset:  selected.Identity().ID,
				ModelID: selectedClientRunModelID(model),
			})
		}
	case "copy":
		copyValue := strings.TrimSpace(action.Content)
		if copyValue != "" {
			actions = append(actions, state.ClientBaseURLCopyRequested{Value: copyValue})
		}
	}
	if len(actions) == 0 {
		return nil
	}
	return actions
}

func selectedClientRunModelID(model state.Model) string {
	snapshot := selectors.CurrentEndpointSnapshot(model)
	if snapshot == nil {
		return ""
	}
	if strings.TrimSpace(snapshot.Name) == "" {
		return ""
	}
	return "primary"
}
