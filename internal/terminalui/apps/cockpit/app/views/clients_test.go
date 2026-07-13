package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/reconcile"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func TestSelectedClientRunModelID_AlwaysUsesPublicSwobuModel(t *testing.T) {
	model := state.Model{
		CurrentEndpoint: "alpha",
		EndpointSnapshots: []state.EndpointSnapshot{
			{
				Name:                      "alpha",
				SelectedProviderConfigRef: "backend-a",
				ProviderConfigs: []state.ProviderConfigSnapshot{
					{
						Ref:          "backend-a",
						ProviderSpec: "openrouter",
						ModelID:      "llama3.2:1b",
						TargetAlias:  "fast",
					},
				},
			},
		},
	}
	if got := selectedClientRunModelID(model); got != exchange.PublicModelIDSwobu {
		t.Fatalf("run model id = %q, want %q", got, exchange.PublicModelIDSwobu)
	}
}

func TestSelectedClientRunModelID_EmptyWithoutWorkspaceSnapshot(t *testing.T) {
	if got := selectedClientRunModelID(state.Model{}); got != "" {
		t.Fatalf("run model id = %q, want empty", got)
	}
}

func TestToggleClientPicker_OpensAtCurrentSelection(t *testing.T) {
	t.Parallel()

	profiles := clientprofile.Catalog()
	selected := selectedClientProfile(profiles, "claude")
	cursor := -1
	open := false
	local := clientsSectionState{
		clientPickerOpen: false,
		setClientPickerOpen: func(next bool) {
			open = next
		},
		setClientPickerCursor: func(next int) {
			cursor = next
		},
	}

	actions := toggleClientPicker(clientPickerFocusKey(selected), clientPickerCursorForSelection(profiles, selected), local)
	if !open {
		t.Fatal("client picker should open")
	}
	if cursor != 1 {
		t.Fatalf("client picker cursor = %d, want 1", cursor)
	}
	if len(actions) != 2 {
		t.Fatalf("actions len=%d want 2", len(actions))
	}
	focus, ok := actions[0].(interaction.FocusKeyAction)
	if !ok {
		t.Fatalf("action[0]=%T want interaction.FocusKeyAction", actions[0])
	}
	if want := clientPickerFocusKey(selected); focus.Key != want {
		t.Fatalf("focus key=%q want %q", focus.Key, want)
	}
	mode, ok := actions[1].(state.SetInteractionMode)
	if !ok {
		t.Fatalf("action[1]=%T want state.SetInteractionMode", actions[1])
	}
	if mode.Mode != state.InteractionModePickOne {
		t.Fatalf("mode=%q want %q", mode.Mode, state.InteractionModePickOne)
	}
}

func TestClientPickerFocusKey_UsesStableProfileIdentity(t *testing.T) {
	t.Parallel()

	first := stubClientProfile{id: "claude", label: "Claude"}
	second := stubClientProfile{id: "claude", label: "Claude Code"}
	if got, want := clientPickerFocusKey(first), "client-picker/claude"; got != want {
		t.Fatalf("focus key = %q, want %q", got, want)
	}
	if got, want := clientPickerFocusKey(second), "client-picker/claude"; got != want {
		t.Fatalf("focus key = %q, want %q", got, want)
	}
}

func TestActionRowFocusKey_UsesStableActionIdentity(t *testing.T) {
	t.Parallel()

	seen := map[string]int{}
	first := actionRowFocusKey(clientprofile.Action{ID: "run", Label: "Launch"}, seen)
	second := actionRowFocusKey(clientprofile.Action{ID: "run", Label: "Copy"}, seen)
	if got, want := first, "client-action/run"; got != want {
		t.Fatalf("first key = %q, want %q", got, want)
	}
	if got, want := second, "client-action/run/1"; got != want {
		t.Fatalf("second key = %q, want %q", got, want)
	}
}

func TestBuildClientRow_RendersCoreBackedSummaryAndOpensPickerOnEnter(t *testing.T) {
	t.Parallel()

	profiles := clientprofile.Catalog()
	selected := selectedClientProfile(profiles, "claude")
	summary := "  Claude  "

	var open bool
	var cursor int
	local := clientsSectionState{
		clientPickerOpen: false,
		setClientPickerOpen: func(next bool) {
			open = next
		},
		setClientPickerCursor: func(next int) {
			cursor = next
		},
	}

	ctx := &retained.Context[state.Model]{
		Local: reconcile.NewLocalStore().Scope(1),
		Model: func() state.Model { return state.Model{} },
	}
	node := retained.Materialize(ctx, buildClientRow(profiles, summary, selected, local))
	if node == nil {
		t.Fatal("expected render node")
	}

	tree := (&layout.TreeBuilder{}).Build(node, geom.Rect{W: 80, H: 4})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 4})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := strings.TrimSpace(buf.String())
	for _, want := range []string{"client", "Claude", "choose"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render = %q, want %q", out, want)
		}
	}

	handler, ok := node.(interaction.ScopedEventHandler)
	if !ok {
		t.Fatalf("render node = %T, want interaction.ScopedEventHandler", node)
	}
	handled, actions := handler.HandleScopedEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEnter}, nil)
	if !handled {
		t.Fatal("enter should be handled by the summary row")
	}
	if !open {
		t.Fatal("client picker should open")
	}
	if cursor != 1 {
		t.Fatalf("client picker cursor = %d, want 1", cursor)
	}
	if len(actions) != 2 {
		t.Fatalf("actions len=%d want 2", len(actions))
	}
	focus, ok := actions[0].(interaction.FocusKeyAction)
	if !ok {
		t.Fatalf("action[0]=%T want interaction.FocusKeyAction", actions[0])
	}
	if want := clientPickerFocusKey(selected); focus.Key != want {
		t.Fatalf("focus key=%q want %q", focus.Key, want)
	}
	mode, ok := actions[1].(state.SetInteractionMode)
	if !ok {
		t.Fatalf("action[1]=%T want state.SetInteractionMode", actions[1])
	}
	if mode.Mode != state.InteractionModePickOne {
		t.Fatalf("mode=%q want %q", mode.Mode, state.InteractionModePickOne)
	}
}

func TestBuildClientPickerRow_ChoosesOnEnterAndSpace(t *testing.T) {
	t.Parallel()

	profiles := clientprofile.Catalog()
	choice := selectedClientProfile(profiles, "claude")
	if choice == nil {
		t.Fatal("expected client profile choice")
	}

	for _, key := range []interaction.Key{interaction.KeyEnter, interaction.KeySpace} {
		key := key
		t.Run(key.String(), func(t *testing.T) {
			t.Parallel()

			var open bool = true
			var expanded string = "client-action/open"
			var scroll int = 7
			local := clientsSectionState{
				clientPickerOpen: true,
				setClientPickerOpen: func(next bool) {
					open = next
				},
				setExpandedActionID: func(next string) {
					expanded = next
				},
				setPayloadScrollOffset: func(next int) {
					scroll = next
				},
			}

			ctx := &retained.Context[state.Model]{
				Local: reconcile.NewLocalStore().Scope(1),
				Model: func() state.Model { return state.Model{} },
			}
			node := retained.Materialize(ctx, buildClientPickerRow(choice, local))
			if node == nil {
				t.Fatal("expected render node")
			}

			tree := (&layout.TreeBuilder{}).Build(node, geom.Rect{W: 80, H: 2})
			buf := paint.NewBuffer(geom.Rect{W: 80, H: 2})
			paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
			out := strings.TrimSpace(buf.String())
			if !strings.Contains(out, choice.Identity().Label) {
				t.Fatalf("render = %q, want %q", out, choice.Identity().Label)
			}

			handler, ok := node.(interaction.ScopedEventHandler)
			if !ok {
				t.Fatalf("render node = %T, want interaction.ScopedEventHandler", node)
			}
			handled, actions := handler.HandleScopedEvent(interaction.Event{Kind: interaction.EventKey, Key: key}, nil)
			if !handled {
				t.Fatalf("%s should be handled by the picker row", key.String())
			}
			if open {
				t.Fatal("client picker should close")
			}
			if expanded != "" {
				t.Fatalf("expanded action id = %q, want empty after choose", expanded)
			}
			if scroll != 0 {
				t.Fatalf("payload scroll offset = %d, want 0", scroll)
			}
			if len(actions) != 3 {
				t.Fatalf("actions len=%d want 3", len(actions))
			}
			selected, ok := actions[0].(state.SetSelectedClientID)
			if !ok {
				t.Fatalf("action[0]=%T want state.SetSelectedClientID", actions[0])
			}
			if got, want := selected.ID, choice.Identity().ID; got != want {
				t.Fatalf("selected client id = %q, want %q", got, want)
			}
			mode, ok := actions[1].(state.SetInteractionMode)
			if !ok {
				t.Fatalf("action[1]=%T want state.SetInteractionMode", actions[1])
			}
			if mode.Mode != state.InteractionModeNAV {
				t.Fatalf("mode=%q want %q", mode.Mode, state.InteractionModeNAV)
			}
			focus, ok := actions[2].(interaction.FocusKeyAction)
			if !ok {
				t.Fatalf("action[2]=%T want interaction.FocusKeyAction", actions[2])
			}
			if got, want := focus.Key, "client"; got != want {
				t.Fatalf("focus key=%q want %q", got, want)
			}
		})
	}
}

func TestBuildClientRow_CancelsOnEscWhenOpen(t *testing.T) {
	t.Parallel()

	profiles := clientprofile.Catalog()
	choice := selectedClientProfile(profiles, "claude")
	if choice == nil {
		t.Fatal("expected client profile choice")
	}

	var open bool = true
	var expanded string = "client-action/open"
	var scroll int = 7
	local := clientsSectionState{
		clientPickerOpen:   true,
		clientPickerCursor: 1,
		setClientPickerOpen: func(next bool) {
			open = next
		},
		setExpandedActionID: func(next string) {
			expanded = next
		},
		setPayloadScrollOffset: func(next int) {
			scroll = next
		},
	}

	ctx := &retained.Context[state.Model]{
		Local: reconcile.NewLocalStore().Scope(1),
		Model: func() state.Model { return state.Model{} },
	}
	node := retained.Materialize(ctx, buildClientRow(profiles, "  Claude  ", choice, local))
	if node == nil {
		t.Fatal("expected render node")
	}

	handler, ok := node.(interaction.ScopedEventHandler)
	if !ok {
		t.Fatalf("render node = %T, want interaction.ScopedEventHandler", node)
	}
	handled, actions := handler.HandleScopedEvent(interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEsc}, nil)
	if !handled {
		t.Fatal("esc should be handled by the picker row")
	}
	if open {
		t.Fatal("client picker should close")
	}
	if expanded != "client-action/open" {
		t.Fatalf("expanded action id = %q, want unchanged", expanded)
	}
	if scroll != 0 {
		t.Fatalf("payload scroll offset = %d, want 0", scroll)
	}
	if len(actions) != 2 {
		t.Fatalf("actions len=%d want 2", len(actions))
	}
	mode, ok := actions[0].(state.SetInteractionMode)
	if !ok {
		t.Fatalf("action[0]=%T want state.SetInteractionMode", actions[0])
	}
	if mode.Mode != state.InteractionModeNAV {
		t.Fatalf("mode=%q want %q", mode.Mode, state.InteractionModeNAV)
	}
	focus, ok := actions[1].(interaction.FocusKeyAction)
	if !ok {
		t.Fatalf("action[1]=%T want interaction.FocusKeyAction", actions[1])
	}
	if got, want := focus.Key, "client"; got != want {
		t.Fatalf("focus key=%q want %q", got, want)
	}
}

func TestClientSummaryKeyHandler_ClosesPickerOnEscWhenOpen(t *testing.T) {
	t.Parallel()

	profiles := clientprofile.Catalog()
	selected := selectedClientProfile(profiles, "claude")

	open := true
	local := clientsSectionState{
		clientPickerOpen: true,
		setClientPickerOpen: func(next bool) {
			open = next
		},
	}

	handled, actions := clientSummaryKeyHandler(clientPickerFocusKey(selected), clientPickerCursorForSelection(profiles, selected), local)(
		nil,
		interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEsc},
	)
	if !handled {
		t.Fatal("esc should be handled while the picker is open")
	}
	if open {
		t.Fatal("client picker should close")
	}
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
	mode, ok := actions[0].(state.SetInteractionMode)
	if !ok {
		t.Fatalf("action[0]=%T want state.SetInteractionMode", actions[0])
	}
	if mode.Mode != state.InteractionModeNAV {
		t.Fatalf("mode=%q want %q", mode.Mode, state.InteractionModeNAV)
	}
}

func TestClientSummaryKeyHandler_BubblesEscWhenClosed(t *testing.T) {
	t.Parallel()

	profiles := clientprofile.Catalog()
	selected := selectedClientProfile(profiles, "claude")
	local := clientsSectionState{clientPickerOpen: false}

	handled, actions := clientSummaryKeyHandler(clientPickerFocusKey(selected), clientPickerCursorForSelection(profiles, selected), local)(
		nil,
		interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyEsc},
	)
	if handled {
		t.Fatal("esc should bubble while the picker is closed")
	}
	if actions != nil {
		t.Fatalf("actions = %#v, want nil", actions)
	}
}

type stubClientProfile struct {
	id    string
	label string
}

func (s stubClientProfile) Identity() clientprofile.Identity {
	return clientprofile.Identity{ID: s.id, Label: s.label}
}

func (s stubClientProfile) Actions(string) []clientprofile.Action { return nil }
