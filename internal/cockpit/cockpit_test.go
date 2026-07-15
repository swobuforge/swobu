package cockpit

import (
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestCockpit_KeyMapOwnsGlobalNavigationOnly(t *testing.T) {
	cockpit := NewCockpit(DefaultFixtureReadModel())
	keymap := cockpit.KeyMap()

	for _, event := range []tui.KeyEvent{
		{Key: tui.KeyTab},
		{Key: tui.KeyTab, Mod: tui.ModShift},
		{Key: tui.KeyRune, Rune: '?'},
		{Key: tui.KeyRune, Rune: 'q'},
	} {
		binding, ok := findRootBinding(keymap, event)
		if !ok {
			t.Fatalf("missing root binding for %#v", event)
		}
		if !binding.Stop {
			t.Fatalf("root binding for %#v should stop propagation", event)
		}
		if binding.Pattern.FocusRequired {
			t.Fatalf("root binding for %#v should not require focus", event)
		}
	}

	for _, key := range []tui.Key{tui.KeyUp, tui.KeyDown, tui.KeyEnter, tui.KeyEscape} {
		if _, ok := findRootBinding(keymap, tui.KeyEvent{Key: key}); ok {
			t.Fatalf("root should not own surface key %v", key)
		}
	}
}

func TestCockpit_TabNavigationSwitchesVisibleWorlds(t *testing.T) {
	cockpit := NewCockpit(DefaultFixtureReadModel())

	pressRootKey(t, cockpit, tui.KeyEvent{Key: tui.KeyTab})
	assertActiveTab(t, cockpit, "lab", readmodel.CockpitWorkspacePage)
	assertRenderContains(t, cockpit, "[› lab]", "workspace")

	pressRootKey(t, cockpit, tui.KeyEvent{Key: tui.KeyTab})
	assertActiveTab(t, cockpit, "+", readmodel.CockpitWorkspacePage)
	assertRenderContains(t, cockpit, "[› +]", "create ↵", "(derived from slug)")

	pressRootKey(t, cockpit, tui.KeyEvent{Key: tui.KeyTab})
	assertActiveTab(t, cockpit, "?", readmodel.CockpitHelpPage)
	assertRenderContains(t, cockpit, "[› ?]", "help")

	pressRootKey(t, cockpit, tui.KeyEvent{Key: tui.KeyTab})
	assertActiveTab(t, cockpit, "dev", readmodel.CockpitWorkspacePage)
	assertRenderContains(t, cockpit, "[› dev]", "routes")
}

func TestCockpit_ShiftTabNavigationWrapsBackward(t *testing.T) {
	cockpit := NewCockpit(DefaultFixtureReadModel())

	pressRootKey(t, cockpit, tui.KeyEvent{Key: tui.KeyTab, Mod: tui.ModShift})
	assertActiveTab(t, cockpit, "?", readmodel.CockpitHelpPage)
	assertRenderContains(t, cockpit, "[› ?]", "help")
}

func TestCockpit_QuestionActivatesHelpWorld(t *testing.T) {
	cockpit := NewCockpit(DefaultFixtureReadModel())

	pressRootKey(t, cockpit, tui.KeyEvent{Key: tui.KeyRune, Rune: '?'})
	assertActiveTab(t, cockpit, "?", readmodel.CockpitHelpPage)
	assertRenderContains(t, cockpit, "[› ?]", "help")
}

func TestCockpit_QuitShortcutIsRootFallback(t *testing.T) {
	cockpit := NewCockpit(DefaultFixtureReadModel())
	binding, ok := findRootBinding(cockpit.KeyMap(), tui.KeyEvent{Key: tui.KeyRune, Rune: 'q'})
	if !ok {
		t.Fatal("missing q binding")
	}
	if !binding.Stop {
		t.Fatal("q binding should stop propagation")
	}

	binding.Handler(tui.KeyEvent{Key: tui.KeyRune, Rune: 'q'})
}

func pressRootKey(t *testing.T, cockpit *Cockpit, event tui.KeyEvent) {
	t.Helper()
	for _, binding := range cockpit.KeyMap() {
		if rootBindingMatches(binding.Pattern, event) {
			binding.Handler(event)
			return
		}
	}
	t.Fatalf("no root binding matched %#v", event)
}

func assertActiveTab(t *testing.T, cockpit *Cockpit, wantID readmodel.WorkspaceID, wantPage readmodel.CockpitPage) {
	t.Helper()
	model := cockpit.activeModel()
	if model.SelectedWorkspaceID != wantID {
		t.Fatalf("selected tab = %q, want %q", model.SelectedWorkspaceID, wantID)
	}
	if model.ActivePage != wantPage {
		t.Fatalf("active page = %v, want %v", model.ActivePage, wantPage)
	}
}

func assertRenderContains(t *testing.T, cockpit *Cockpit, values ...string) {
	t.Helper()
	got := testkit.RenderString(cockpit.Render(nil), 100, 24)
	for _, value := range values {
		if !strings.Contains(got, value) {
			t.Fatalf("render should contain %q:\n%s", value, got)
		}
	}
}

func findRootBinding(keymap tui.KeyMap, event tui.KeyEvent) (tui.KeyBinding, bool) {
	for _, binding := range keymap {
		if rootBindingMatches(binding.Pattern, event) {
			return binding, true
		}
	}
	return tui.KeyBinding{}, false
}

func rootBindingMatches(pattern tui.KeyPattern, event tui.KeyEvent) bool {
	if pattern.ExcludeMods != 0 && event.Mod&pattern.ExcludeMods != 0 {
		return false
	}
	if pattern.Mod != 0 && event.Mod != pattern.Mod {
		return false
	}
	if pattern.Rune != 0 {
		return event.Key == tui.KeyRune && event.Rune == pattern.Rune
	}
	return pattern.Key != 0 && event.Key == pattern.Key
}
