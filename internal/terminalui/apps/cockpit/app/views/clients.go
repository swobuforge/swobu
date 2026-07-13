// Clients section retained, with static fallback summaries and picker option
// rows lowered through core. Selected-client identity is model-owned; the
// interactive summary row and picker option bodies are core-backed, while the
// remaining disclosure shell keeps action-disclosure state in the model.
package views

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildClientsSectionNode returns the clients section as a core.Node.
func BuildClientsSectionNode(model state.Model) core.Node[state.Action] {
	selected := selectedClientProfile(clientprofile.Catalog(), model.SelectedClientID)
	summary := clientsSummaryLabel(selected)
	if model.InteractionMode == state.InteractionModePickOne {
		return BuildClientsInteractiveSummaryNode(summary)
	}
	return BuildClientsStaticSummaryNode()
}

// BuildClientsSection composes the clients section rows (retained bridge).
// TODO(v2-migration): migrate action rows to core.Node once toolkit render
// nodes have semantic equivalents.
func BuildClientsSection(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	if spec, ok := maybeStaticClientsSection(ctx, model); ok {
		return spec
	}
	baseURL := strings.TrimSpace(selectors.ClientBaseURL(model)) // swobu:io-string source=boundary
	profiles := clientprofile.Catalog()
	selected := selectedClientProfile(profiles, model.SelectedClientID)
	summary := clientsSummaryLabel(selected)

	clientRow := buildClientRowRetained(profiles, summary, selected, model)
	actions := selectedClientActions(selected, baseURL)
	rows := []retained.ViewSpec[state.Model]{retained.Named[state.Model]("client", clientRow)}
	rows = append(rows, buildActionRowsRetained(model, actions, baseURL, selected)...)
	return retained.Named[state.Model](
		SectionClients,
		retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
			m := ctx.Model()
			open := m.SectionOpen[SectionClients]
			closeSection := func() []update.Action {
				if !open {
					return nil
				}
				return []update.Action{
					state.ToggleSectionOpen{Title: SectionClients},
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
					return []update.Action{
						state.ToggleSectionOpen{Title: SectionClients},
						interaction.FocusKeyAction{Key: "client"},
					}
				}),
			)
			clientRowFresh := buildClientRowRetained(profiles, summary, selected, m)
			actionRows := buildActionRowsRetained(m, actions, baseURL, selected)
			children := []retained.ViewSpec[state.Model]{titleRow}
			if open {
				children = append(children, retained.Named[state.Model]("client", clientRowFresh))
				children = append(children, actionRows...)
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
			return clientsStaticSummarySection(ctx), true
		}
		return NewCollapsibleSection(
			SectionClients,
			false,
			"open",
			CoreNodeAsRetained(BuildClientsStaticSummaryNode()),
		), true
	}
	if model.HeaderStatus == "saved" {
		return clientsStaticSummarySection(ctx), true
	}
	return nil, false
}

// BuildClientsStaticSummaryNode returns the static fallback summary row as one
// pure semantic core.Node.
func BuildClientsStaticSummaryNode() core.Node[state.Action] {
	return clientsStaticSummaryLineNode("not set")
}

// BuildClientsInteractiveSummaryNode returns the interactive clients summary
// row body as one pure semantic core.Node.
func BuildClientsInteractiveSummaryNode(summary string) core.Node[state.Action] {
	line := fmt.Sprintf("%-17s  %s  choose ↵", "client", strings.TrimSpace(summary)) // swobu:io-string source=boundary
	activation := core.SignalEvent[state.Action]{
		Kind:  cockpitActionSignalKind,
		Event: state.SetInteractionMode{Mode: state.InteractionModePickOne},
	}
	node := core.Action[state.Action](line, activation).Key(core.K("client"))
	return node.Interaction(core.InteractionSpec[state.Action]{
		Focus: core.FocusSpec{Mode: core.Focusable},
		// The retained wrapper owns Enter/Esc here; this inert binding keeps the
		// core node valid without stealing the row's runtime activation path.
		Keymap:       []core.KeyBindingSpec{{Pattern: core.KeyMatch{Name: "noop"}, Intent: core.IntentActivate}},
		Help:         []core.HelpBindingSpec{{Key: "↵", Label: "choose"}},
		Signals:      []core.SignalEvent[state.Action]{activation},
		FocusSignals: []core.SignalEvent[state.Action]{{Kind: cockpitRowFocusSignalKind, Event: state.SetFocusedRowAffordance{Verb: "choose", AllowSpace: false}}},
	})
}

func clientsStaticSummarySection(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	return retained.VStack(ctx,
		sectionStaticTitleRow(SectionClients, false),
		CoreNodeAsRetained(BuildClientsStaticSummaryNode()),
	)
}

func clientsStaticSummaryLineNode(summary string) core.Node[state.Action] {
	return core.Text[state.Action](strings.Repeat(" ", InsetDetail) + strings.TrimSpace(summary))
}

func buildClientRowRetained(profiles []clientprofile.Profile, summary string, selected clientprofile.Profile, model state.Model) retained.ViewSpec[state.Model] {
	selectedCursor := clientPickerCursorForSelection(profiles, selected)
	selectedFocusKey := clientPickerFocusKey(selected)
	if selectedCursor >= 0 && selectedCursor < len(profiles) {
		selectedFocusKey = clientPickerFocusKey(profiles[selectedCursor])
	}
	summaryRow := toolkitviews.KeyScope(
		CoreNodeAsRetained(BuildClientsInteractiveSummaryNode(summary)),
		clientSummaryKeyHandlerRetained(selectedFocusKey, selectedCursor, model),
	)
	if !model.ClientPickerOpen {
		return summaryRow
	}
	return buildClientPickerDisclosureRetained(summaryRow, profiles, model)
}

func buildClientPickerDisclosureRetained(summaryRow retained.ViewSpec[state.Model], profiles []clientprofile.Profile, model state.Model) retained.ViewSpec[state.Model] {
	options := buildClientPickerRowsRetained(profiles, model)
	optionStack := retained.VStack[state.Model](nil, options...)
	optionViewport := retained.Constrain[state.Model](
		retained.ScrollY[state.Model](optionStack, 0),
		retained.ConstrainSpec{
			GrowW: true,
			MaxW:  ContentMaxWidth,
			MaxH:  ListMaxHeight,
		},
	)
	disclosure := EscClosableDisclosure(summaryRow, model.ClientPickerOpen, func() []update.Action {
		return cancelClientPickerOptionRetained(model)
	}, optionViewport)
	pickerKeyHandler := clientPickerKeyHandlerRetained(profiles, model)
	return retained.View[state.Model](func(ctx *retained.Context[state.Model]) retained.RenderNode {
		child := retained.Materialize(ctx, disclosure)
		scope := toolkitviews.NewKeyScope(child, func(ev interaction.Event) (bool, []update.Action) {
			if pickerKeyHandler == nil {
				return false, nil
			}
			return pickerKeyHandler(ctx, ev)
		})
		scope.Fallback = func(ev interaction.Event) (bool, []update.Action) {
			if scoped, ok := child.(interaction.ScopedEventHandler); ok {
				return scoped.HandleScopedEvent(ev, nil)
			}
			return false, nil
		}
		return scope
	})
}

func clientSummaryKeyHandlerRetained(selectedFocusKey string, selectedCursor int, model state.Model) func(*retained.Context[state.Model], interaction.Event) (bool, []update.Action) {
	return func(_ *retained.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
		if ev.Kind != interaction.EventKey {
			return false, nil
		}
		switch ev.Key {
		case interaction.KeyEnter:
			return true, toggleClientPickerAction(selectedFocusKey, selectedCursor, model)
		case interaction.KeyEsc:
			if !model.ClientPickerOpen {
				return false, nil
			}
			return true, closeClientPickerAction(model)
		default:
			return false, nil
		}
	}
}

func toggleClientPickerAction(selectedFocusKey string, selectedCursor int, model state.Model) []update.Action {
	nextOpen := !model.ClientPickerOpen
	if !nextOpen {
		return []update.Action{
			state.SetClientPickerOpen{Open: false},
			state.SetInteractionMode{Mode: state.InteractionModeNAV},
		}
	}
	if selectedCursor < 0 {
		selectedCursor = 0
	}
	return []update.Action{
		state.SetClientPickerOpen{Open: true},
		state.SetClientPickerCursor{Cursor: selectedCursor},
		interaction.FocusKeyAction{Key: selectedFocusKey},
		state.SetInteractionMode{Mode: state.InteractionModePickOne},
	}
}

func closeClientPickerAction(model state.Model) []update.Action {
	if !model.ClientPickerOpen {
		return nil
	}
	return []update.Action{
		state.SetClientPickerOpen{Open: false},
		state.SetPayloadScrollOffset{Offset: 0},
		state.SetInteractionMode{Mode: state.InteractionModeNAV},
	}
}

// buildClientPickerOptionNode returns one semantic picker option row body.
func buildClientPickerOptionNode(profile clientprofile.Profile) core.Node[state.Action] {
	identity := profile.Identity()
	label := toolkitviews.InsetLabel(identity.Label, 4)
	activation := core.SignalEvent[state.Action]{
		Kind:  cockpitActionSignalKind,
		Event: state.SetSelectedClientID{ID: identity.ID},
	}
	return core.Action[state.Action](label, activation).
		Key(core.K(clientPickerFocusKey(profile))).
		Interaction(core.InteractionSpec[state.Action]{
			Focus: core.FocusSpec{Mode: core.Focusable},
			// The retained key scope owns Enter/Space/Esc here; the inert binding
			// keeps the node valid without stealing the runtime picker shortcut path.
			Keymap:  []core.KeyBindingSpec{{Pattern: core.KeyMatch{Name: "noop"}, Intent: core.IntentActivate}},
			Help:    []core.HelpBindingSpec{{Key: "↵", Label: "select"}},
			Signals: []core.SignalEvent[state.Action]{activation},
			FocusSignals: []core.SignalEvent[state.Action]{
				{Kind: cockpitRowFocusSignalKind, Event: state.SetFocusedRowAffordance{Verb: "select", AllowSpace: false}},
			},
		})
}

func buildClientPickerRowsRetained(profiles []clientprofile.Profile, model state.Model) []retained.ViewSpec[state.Model] {
	pickerRows := make([]retained.ViewSpec[state.Model], 0, len(profiles))
	for _, profile := range profiles {
		pickerRows = append(pickerRows, buildClientPickerRowRetained(profile, model))
	}
	return pickerRows
}

func buildClientPickerRowRetained(profile clientprofile.Profile, model state.Model) retained.ViewSpec[state.Model] {
	choice := profile
	return retained.Named[state.Model](clientPickerFocusKey(choice),
		toolkitviews.KeyScope(
			CoreNodeAsRetained(buildClientPickerOptionNode(choice)),
			clientPickerOptionKeyHandlerRetained(choice, model),
		),
	)
}

func clientPickerOptionKeyHandlerRetained(choice clientprofile.Profile, model state.Model) func(*retained.Context[state.Model], interaction.Event) (bool, []update.Action) {
	return func(_ *retained.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
		if ev.Kind != interaction.EventKey {
			return false, nil
		}
		switch ev.Key {
		case interaction.KeyEnter, interaction.KeySpace:
			return true, chooseClientPickerOptionRetained(choice, model)
		case interaction.KeyEsc:
			return true, cancelClientPickerOptionRetained(model)
		default:
			return false, nil
		}
	}
}

func chooseClientPickerOptionRetained(choice clientprofile.Profile, _ state.Model) []update.Action {
	return []update.Action{
		state.SetSelectedClientID{ID: choice.Identity().ID},
		state.SetClientPickerOpen{Open: false},
		state.ToggleExpandedActionID{ActionID: ""},
		state.SetPayloadScrollOffset{Offset: 0},
		state.SetInteractionMode{Mode: state.InteractionModeNAV},
		interaction.FocusKeyAction{Key: "client"},
	}
}

func cancelClientPickerOptionRetained(_ state.Model) []update.Action {
	return []update.Action{
		state.SetClientPickerOpen{Open: false},
		state.SetPayloadScrollOffset{Offset: 0},
		state.SetInteractionMode{Mode: state.InteractionModeNAV},
		interaction.FocusKeyAction{Key: "client"},
	}
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

func clientPickerKeyHandlerRetained(profiles []clientprofile.Profile, model state.Model) func(*retained.Context[state.Model], interaction.Event) (bool, []update.Action) {
	return func(_ *retained.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
		if !model.ClientPickerOpen || ev.Kind != interaction.EventKey || len(profiles) == 0 {
			return false, nil
		}
		if ev.Key == interaction.KeyUp {
			return moveClientPickerCursorAction(profiles, model, -1)
		}
		if ev.Key == interaction.KeyDown {
			return moveClientPickerCursorAction(profiles, model, 1)
		}
		return false, nil
	}
}

func moveClientPickerCursorAction(profiles []clientprofile.Profile, model state.Model, delta int) (bool, []update.Action) {
	next := model.ClientPickerCursor + delta
	if next < 0 || next >= len(profiles) {
		return true, nil
	}
	return true, []update.Action{
		state.SetClientPickerCursor{Cursor: next},
		interaction.FocusKeyAction{Key: clientPickerFocusKey(profiles[next])},
	}
}

func buildActionRowsRetained(model state.Model, actions []clientprofile.Action, baseURL string, selected clientprofile.Profile) []retained.ViewSpec[state.Model] {
	rows := make([]retained.ViewSpec[state.Model], 0, len(actions))
	seen := map[string]int{}
	for _, action := range actions {
		rowKey := actionRowFocusKeyRetained(action, seen)
		row := buildActionRowRetained(model, action, baseURL, selected)
		rows = append(rows, retained.Named[state.Model](rowKey, row))
	}
	return rows
}

func actionRowFocusKeyRetained(action clientprofile.Action, seen map[string]int) string {
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

func actionStableID(action clientprofile.Action) string {
	id := strings.TrimSpace(action.ID) // swobu:io-string source=boundary
	if id == "" {
		id = action.RowLabel()
	}
	if id == "" {
		id = "action"
	}
	return id
}

func buildActionRowRetained(model state.Model, action clientprofile.Action, baseURL string, selected clientprofile.Profile) retained.ViewSpec[state.Model] {
	const payloadMaxHeight = 8
	actionID := actionStableID(action)
	row := RowActionWithHooks(action.RowLabel(), action.ActionSummary(), action.ActionVerb(), func() []update.Action {
		return activateClientActionRetained(model, action, actionID, baseURL, selected)
	}, func() []update.Action {
		if model.ExpandedActionID != actionID {
			return nil
		}
		return []update.Action{
			state.ToggleExpandedActionID{ActionID: actionID},
			state.SetPayloadScrollOffset{Offset: 0},
		}
	}, func() []update.Action {
		return focusAffordance(action.ActionVerb(), false)()
	})
	note := actionResultNoteRetained(model, action)
	if model.ExpandedActionID != actionID || !action.HasPayload() {
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
		model.PayloadScrollOffset,
		model.PayloadScrollOffset > 0,
		model.PayloadScrollOffset < maxOffset,
		rows...,
	)
	return keyScopeForDisclosureScroll(disclosure, model, maxOffset)
}

func actionResultNoteRetained(model state.Model, action clientprofile.Action) string {
	if action.ActionVerb() == "run" {
		return model.ClientLaunchNote
	}
	return ""
}

func activateClientActionRetained(model state.Model, action clientprofile.Action, actionID, baseURL string, selected clientprofile.Profile) []update.Action {
	if action.HasPayload() {
		if model.ExpandedActionID != actionID {
			return []update.Action{
				state.ToggleExpandedActionID{ActionID: actionID},
				state.SetPayloadScrollOffset{Offset: 0},
			}
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

// actionRowFocusKey is the canonical name used by tests; delegates to the
// retained bridge implementation.
func actionRowFocusKey(action clientprofile.Action, seen map[string]int) string {
	return actionRowFocusKeyRetained(action, seen)
}

func contentRows(content string) []retained.ViewSpec[state.Model] {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	out := make([]retained.ViewSpec[state.Model], 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" { // swobu:io-string source=boundary
			continue
		}
		out = append(out, payloadTextRow(line))
	}
	return out
}

func payloadMaxOffset(rowCount, maxHeight int) int {
	if rowCount <= maxHeight {
		return 0
	}
	return rowCount - maxHeight
}

func clientPickerFocusKey(profile clientprofile.Profile) string {
	id := ""
	if profile != nil {
		identity := profile.Identity()
		id = identity.ID
	}
	if id == "" {
		id = "client"
	}
	return "client-picker/" + id
}
