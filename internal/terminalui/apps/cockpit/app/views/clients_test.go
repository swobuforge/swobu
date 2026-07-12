package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/clientprofile"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
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
		selectedClientID: "claude",
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
	if got, want := clientPickerFocusKey(first), "client-option/claude"; got != want {
		t.Fatalf("focus key = %q, want %q", got, want)
	}
	if got, want := clientPickerFocusKey(second), "client-option/claude"; got != want {
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

type stubClientProfile struct {
	id    string
	label string
}

func (s stubClientProfile) Identity() clientprofile.Identity {
	return clientprofile.Identity{ID: s.id, Label: s.label}
}

func (s stubClientProfile) Actions(string) []clientprofile.Action { return nil }
