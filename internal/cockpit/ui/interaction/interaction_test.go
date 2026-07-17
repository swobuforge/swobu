package interaction

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestSelectable_ImplementsFocusedDispatchContract(t *testing.T) {
	row := NewSelectable(SelectableProps{ID: "row"})
	if row.IsFocused() {
		t.Fatal("new selectable should not start focused")
	}

	row.Cell.OnFocus(nil)
	if !row.IsFocused() {
		t.Fatal("focused selectable must report IsFocused for tui.OnFocused")
	}

	row.Cell.OnBlur(nil)
	if row.IsFocused() {
		t.Fatal("blurred selectable should not report focused")
	}
}

func TestSelectable_KeyMapUsesActivationEscapeThenTraversal(t *testing.T) {
	row := NewSelectable(SelectableProps{
		ID:       "row",
		OnEscape: func(Context) {},
	})

	keymap := row.KeyMap()
	if len(keymap) != 5 {
		t.Fatalf("keymap length = %d, want activation(2)+escape+traversal(2)", len(keymap))
	}
	assertFocusedStop(t, keymap[0], tui.KeyEnter)
	assertFocusedStop(t, keymap[2], tui.KeyEscape)
	assertFocusedStop(t, keymap[3], tui.KeyDown)
	assertFocusedStop(t, keymap[4], tui.KeyUp)
}

func TestSelectable_KeyMapOmitsEscapeWhenNoConsumer(t *testing.T) {
	row := NewSelectable(SelectableProps{ID: "row"})

	keymap := row.KeyMap()
	if len(keymap) != 4 {
		t.Fatalf("keymap length = %d, want activation(2)+traversal(2)", len(keymap))
	}
	for _, binding := range keymap {
		if binding.Pattern.Key == tui.KeyEscape {
			t.Fatal("selectable without local Escape consumer must not register a stop Escape binding")
		}
	}
}

func TestSelectable_SetRenderPropsDoesNotAutofocus(t *testing.T) {
	row := NewSelectable(SelectableProps{ID: "row"})

	row.SetRenderProps(SelectableProps{ID: "row", AutoFocus: true})

	if row.IsFocused() {
		t.Fatal("SetRenderProps must not perform autofocus repair")
	}
}

func TestSelectable_UpdateMayAutofocus(t *testing.T) {
	row := NewSelectable(SelectableProps{ID: "row"})

	row.Update(SelectableProps{ID: "row", AutoFocus: true})

	if !row.IsFocused() {
		t.Fatal("Update should preserve lifecycle autofocus transition behavior")
	}
}

func TestBackScope_OmitsEscapeWhenInactive(t *testing.T) {
	keymap := BackScope(func() bool { return false }, func(Context) {})
	if len(keymap) != 0 {
		t.Fatalf("inactive back scope keymap len = %d, want 0", len(keymap))
	}
}

func TestBackScope_PreemptsEscapeWhenActive(t *testing.T) {
	keymap := BackScope(func() bool { return true }, func(Context) {})
	if len(keymap) != 1 {
		t.Fatalf("active back scope keymap len = %d, want 1", len(keymap))
	}
	binding := keymap[0]
	if binding.Pattern.Key != tui.KeyEscape {
		t.Fatalf("back scope key = %v, want Escape", binding.Pattern.Key)
	}
	if binding.Pattern.FocusRequired {
		t.Fatal("back scope Escape must not be focus-gated")
	}
	if !binding.Preempt || !binding.Stop {
		t.Fatal("back scope Escape must preempt-stop so scope back wins before parent fallback")
	}
}

func TestDisclosure_EscapeCollapsesOnlyWhenExpanded(t *testing.T) {
	expanded := tui.NewState(false)
	var collapsed int
	disclosure := NewDisclosure(DisclosureProps{
		ID:       "section",
		Expanded: expanded,
		OnCollapse: func(Context) {
			collapsed++
		},
	})
	root := &disclosureFallbackRoot{disclosure: disclosure}
	h := newInteractionHarness(t, root)

	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if expanded.Get() {
		t.Fatal("collapsed disclosure should stay collapsed after Escape")
	}
	if collapsed != 0 {
		t.Fatalf("collapsed disclosure callbacks = %d, want 0", collapsed)
	}
	if root.escapes != 1 {
		t.Fatalf("collapsed disclosure should bubble Escape to parent; parent calls = %d, want 1", root.escapes)
	}

	expanded.Set(true)
	h.Frame()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if expanded.Get() {
		t.Fatal("escape should collapse expanded disclosure")
	}
	if collapsed != 1 {
		t.Fatalf("collapse callbacks = %d, want 1", collapsed)
	}
	if root.escapes != 1 {
		t.Fatalf("expanded disclosure should consume Escape before parent fallback; parent calls = %d, want 1", root.escapes)
	}
}

type disclosureFallbackRoot struct {
	disclosure *Disclosure
	escapes    int
}

func (r *disclosureFallbackRoot) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100.00),
	)
	root.AddChild(app.Mount(r, 0, func() tui.Component { return r.disclosure }))
	return root
}

func (r *disclosureFallbackRoot) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyEscape, func(tui.KeyEvent) { r.escapes++ }),
	}
}

func newInteractionHarness(t *testing.T, root tui.Component) *testkit.MockAppHarness {
	t.Helper()

	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	h.Open()
	t.Cleanup(h.Close)
	return h
}

func assertFocusedStop(t *testing.T, binding tui.KeyBinding, key tui.Key) {
	t.Helper()
	if binding.Pattern.Key != key {
		t.Fatalf("binding key = %v, want %v", binding.Pattern.Key, key)
	}
	if !binding.Pattern.FocusRequired {
		t.Fatalf("binding %v is not focus-gated", key)
	}
	if !binding.Stop {
		t.Fatalf("binding %v does not stop propagation", key)
	}
}
