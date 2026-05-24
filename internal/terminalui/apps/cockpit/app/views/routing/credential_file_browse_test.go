package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestPostCredentialFileSelectionActions_AppendsCloseModeAndFocusModel(t *testing.T) {
	t.Parallel()

	base := []update.Action{state.SetCreateDraftCredentialRef{CredentialRef: "file:/tmp/token.txt"}}
	actions := postCredentialFileSelectionActions(base, state.InteractionModeManageList)
	if len(actions) != 3 {
		t.Fatalf("actions len=%d want 3", len(actions))
	}
	if _, ok := actions[0].(state.SetCreateDraftCredentialRef); !ok {
		t.Fatalf("action[0]=%T want state.SetCreateDraftCredentialRef", actions[0])
	}
	mode, ok := actions[1].(state.SetInteractionMode)
	if !ok {
		t.Fatalf("action[1]=%T want state.SetInteractionMode", actions[1])
	}
	if mode.Mode != state.InteractionModeManageList {
		t.Fatalf("mode=%q want %q", mode.Mode, state.InteractionModeManageList)
	}
	focus, ok := actions[2].(interaction.FocusKeyAction)
	if !ok {
		t.Fatalf("action[2]=%T want interaction.FocusKeyAction", actions[2])
	}
	if focus.Key != "model" {
		t.Fatalf("focus key=%q want model", focus.Key)
	}
}

func TestPostCredentialFileSelectionActions_DoesNotMutateBaseSlice(t *testing.T) {
	t.Parallel()

	base := []update.Action{state.SetCreateDraftCredentialRef{CredentialRef: "file:/tmp/token.txt"}}
	_ = postCredentialFileSelectionActions(base, state.InteractionModeNAV)
	if len(base) != 1 {
		t.Fatalf("base len=%d want 1", len(base))
	}
}
