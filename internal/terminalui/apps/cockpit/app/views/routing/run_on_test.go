package routing

import (
	"testing"

	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
)

func TestRunOnProviderChooseActions_ReselectionClosesWithoutSave(t *testing.T) {
	t.Parallel()

	snapshot := &state.EndpointSnapshot{
		Name:                      "acme",
		SelectedProviderConfigRef: "backend-a",
	}
	closeActions := []update.Action{
		state.SetInteractionMode{Mode: state.InteractionModeNAV},
		interaction.FocusKeyAction{Key: "run_on"},
	}
	got := runOnProviderChooseActions(snapshot, "backend-a", func() []update.Action {
		return closeActions
	})

	if len(got) != len(closeActions) {
		t.Fatalf("action len=%d want=%d", len(got), len(closeActions))
	}
	for _, action := range got {
		switch action.(type) {
		case state.RoutingSaveStartedAction, state.SaveSelectedTargetRequested:
			t.Fatalf("unexpected save action for same provider: %T", action)
		}
	}
}

func TestRunOnProviderChooseActions_SelectionQueuesSaveThenClose(t *testing.T) {
	t.Parallel()

	snapshot := &state.EndpointSnapshot{
		Name:                      "acme",
		SelectedProviderConfigRef: "backend-a",
	}
	got := runOnProviderChooseActions(snapshot, "backend-b", func() []update.Action {
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeNAV},
			interaction.FocusKeyAction{Key: "run_on"},
		}
	})

	if len(got) != 4 {
		t.Fatalf("action len=%d want 4", len(got))
	}
	if _, ok := got[0].(state.RoutingSaveStartedAction); !ok {
		t.Fatalf("action[0]=%T want state.RoutingSaveStartedAction", got[0])
	}
	save, ok := got[1].(state.SaveSelectedTargetRequested)
	if !ok {
		t.Fatalf("action[1]=%T want state.SaveSelectedTargetRequested", got[1])
	}
	if save.EndpointName != "acme" || save.ProviderRef != "backend-b" {
		t.Fatalf("save action = %#v", save)
	}
}
