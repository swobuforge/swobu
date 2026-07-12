package routing

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
)

func TestBedrockProfilePickerToggleActions_OpenSetsPickerFocus(t *testing.T) {
	t.Parallel()

	items := bedrockProfilePickerItems([]string{"swobu-bedrock"}, "", nil)
	actions := bedrockProfilePickerToggleActions(true, state.InteractionModeManageList, items)
	if len(actions) != 2 {
		t.Fatalf("actions len=%d want 2", len(actions))
	}
	mode, ok := actions[0].(state.SetInteractionMode)
	if !ok {
		t.Fatalf("action[0]=%T want state.SetInteractionMode", actions[0])
	}
	if mode.Mode != state.InteractionModePickOne {
		t.Fatalf("mode=%q want %q", mode.Mode, state.InteractionModePickOne)
	}
	focus, ok := actions[1].(interaction.FocusKeyAction)
	if !ok {
		t.Fatalf("action[1]=%T want interaction.FocusKeyAction", actions[1])
	}
	if want := views.FilterablePickerFirstFocusKey(items, views.FilterablePickerConfig{KeyPrefix: "bedrock-profile-option"}); focus.Key != want {
		t.Fatalf("focus key=%q want %q", focus.Key, want)
	}
}

func TestBedrockProfilePickerToggleActions_CloseRestoresCloseMode(t *testing.T) {
	t.Parallel()

	actions := bedrockProfilePickerToggleActions(false, state.InteractionModeManageList, nil)
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
	mode, ok := actions[0].(state.SetInteractionMode)
	if !ok {
		t.Fatalf("action[0]=%T want state.SetInteractionMode", actions[0])
	}
	if mode.Mode != state.InteractionModeManageList {
		t.Fatalf("mode=%q want %q", mode.Mode, state.InteractionModeManageList)
	}
}

func TestBedrockProfilePickerItems_AlwaysIncludesAutoOption(t *testing.T) {
	t.Parallel()

	items := bedrockProfilePickerItems(nil, "", nil)
	if len(items) != 1 {
		t.Fatalf("items len=%d want 1", len(items))
	}
	if items[0].Label != "auto" {
		t.Fatalf("item[0] label=%q", items[0].Label)
	}
	if !items[0].Selected {
		t.Fatal("auto option should be selected when current profile is unset")
	}
}

func TestBedrockProfilePickerItems_ExplicitProfileAndAutoAreBothSelectable(t *testing.T) {
	t.Parallel()

	items := bedrockProfilePickerItems([]string{"default", "swobu-bedrock"}, "swobu-bedrock", nil)
	if len(items) != 3 {
		t.Fatalf("items len=%d want 3", len(items))
	}
	if items[0].Label != "auto" {
		t.Fatalf("item[0] label=%q", items[0].Label)
	}
	if items[0].Selected {
		t.Fatal("auto should not be selected when explicit profile is current")
	}
	if items[1].Label != "default" || items[2].Label != "swobu-bedrock" {
		t.Fatalf("labels=%q,%q", items[1].Label, items[2].Label)
	}
	if !items[2].Selected {
		t.Fatal("explicit current profile should be selected")
	}
}
