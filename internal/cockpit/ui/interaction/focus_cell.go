package interaction

import tui "github.com/grindlemire/go-tui"

// FocusCell is the smallest Cockpit adapter over a go-tui focusable element.
//
// It owns the ref and local marker state required for a mounted component to
// satisfy go-tui's focus-gated dispatch contract. Feature code should not build
// FocusCells directly; higher-level ui controls use it to implement
// selectable rows, disclosures, scoped lists, and editors.
type FocusCell struct {
	ID string

	Ref *tui.Ref

	app     *tui.App
	focused *tui.State[bool]
}

// NewFocusCell creates a focus cell with a stable mount/debug ID.
func NewFocusCell(id string) FocusCell {
	return FocusCell{
		ID:      id,
		Ref:     tui.NewRef(),
		focused: tui.NewState(false),
	}
}

// BindApp wires the focus marker state to the mounted app.
func (c *FocusCell) BindApp(app *tui.App) {
	c.app = app
	if c.focused != nil {
		c.focused.BindApp(app)
	}
}

// UnbindApp releases the app handle when the owner leaves the tree.
func (c *FocusCell) UnbindApp() {
	c.app = nil
}

// OnFocus records that the mounted shell gained focus.
//
// go-tui's focusManager.refreshFromTree (focus.go) runs after every render and
// re-calls Focus() on the focused element to repair the focus graph — re-renders
// build fresh Element objects whose `focused` flag starts false, so Element.Focus's
// own idempotency guard (element_focus.go) never short-circuits and this handler
// fires every frame. Without the value guard below, that would Set(true) on the
// persistent marker state every frame, and State.Set marks the app dirty
// unconditionally — a self-sustaining render loop that re-layouts (re-measuring
// every string width) at 60 fps even while idle. Skip the Set when the marker is
// already correct: the persistent state only needs to change on genuine focus
// transitions, which still route through here with the opposite value.
func (c *FocusCell) OnFocus(*tui.Element) {
	if c.focused != nil && !c.focused.Get() {
		c.focused.Set(true)
	}
}

// OnBlur records that the mounted shell lost focus. See OnFocus: guarded against
// the redundant per-frame Set that would otherwise sustain the idle render loop.
func (c *FocusCell) OnBlur(*tui.Element) {
	if c.focused != nil && c.focused.Get() {
		c.focused.Set(false)
	}
}

// IsFocused satisfies go-tui's focus-gated dispatch contract for components
// that return tui.OnFocused bindings.
func (c *FocusCell) IsFocused() bool {
	if c.Ref == nil {
		return c.focused != nil && c.focused.Get()
	}
	if el := c.Ref.El(); el != nil {
		return el.IsFocused()
	}
	return c.focused != nil && c.focused.Get()
}

// Marker returns the shared Cockpit focus marker for this cell.
func (c *FocusCell) Marker() string {
	if c.IsFocused() {
		return ">"
	}
	return " "
}

// Focus seeds the visible marker and repairs the app focus graph.
//
// This is a one-shot mount/update operation. Calling it from Render or BindApp
// would make render mutate the framework focus graph.
func (c *FocusCell) Focus(app *tui.App) {
	if c.focused != nil {
		c.focused.Set(true)
	}
	FocusRefByTraversal(app, c.Ref)
}

// FocusedState exposes the marker state to templ-generated render deps.
func (c *FocusCell) FocusedState() *tui.State[bool] {
	return c.focused
}
