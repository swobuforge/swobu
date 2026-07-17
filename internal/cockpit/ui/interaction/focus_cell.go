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
func (c *FocusCell) OnFocus(*tui.Element) {
	if c.focused != nil {
		c.focused.Set(true)
	}
}

// OnBlur records that the mounted shell lost focus.
func (c *FocusCell) OnBlur(*tui.Element) {
	if c.focused != nil {
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
