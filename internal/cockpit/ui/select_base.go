package ui

import tui "github.com/grindlemire/go-tui"

// Shared select-flow grammar for row markers.
const (
	SelectArrowFocused = ">"
	SelectArrowBlurred = " "
)

// RowArrow returns the shared marker for a selected row scope.
func RowArrow(active bool) string {
	if active {
		return SelectArrowFocused
	}
	return SelectArrowBlurred
}

// SelectBase provides focus-aware identity and state for selectable cockpit
// rows. It embeds into struct components that participate in the go-tui focus
// graph.
//
// Focus repair (restoring focus after a mount/update transition that orphaned
// the old focus target) is handled by FocusTraversal, not here. SelectBase
// only holds the ref and local state that FocusTraversal consumes.
type SelectBase struct {
	// TODO why do we need ID? should we remove it? Or go-tui uses it?
	ID      string
	Ref     *tui.Ref
	app     *tui.App
	focused *tui.State[bool]
}

// NewSelectBase creates a new SelectBase with the given stable ID.
func NewSelectBase(id string) SelectBase {
	return SelectBase{
		ID:      id,
		Ref:     tui.NewRef(),
		focused: tui.NewState(false),
	}
}

// OnFocus marks the base as focused.
func (b *SelectBase) OnFocus(el *tui.Element) {
	b.focused.Set(true)
}

// OnBlur marks the base as unfocused.
func (b *SelectBase) OnBlur(el *tui.Element) {
	b.focused.Set(false)
}

// IsFocused returns true if the component has been seeded as focused or if
// the resolved go-tui element currently owns focus.
func (b *SelectBase) IsFocused() bool {
	if b.focused != nil && b.focused.Get() {
		return true
	}
	if b.Ref != nil {
		if el := b.Ref.El(); el != nil {
			return el.IsFocused()
		}
	}
	return false
}

// Arrow returns the shared selection marker for the current focus state.
func (b *SelectBase) Arrow() string {
	if b.IsFocused() {
		return SelectArrowFocused
	}
	return SelectArrowBlurred
}

// ArrowWithActiveDescendant returns the shared row marker for a selected row
// scope. The descendant flag covers mounted child controls, such as text
// inputs, that temporarily own keyboard focus while the parent row remains the
// selected interaction scope.
func (b *SelectBase) ArrowWithActiveDescendant(activeDescendant bool) string {
	return RowArrow(b.IsFocused() || activeDescendant)
}

// FocusedState exposes the focus state for reactive render deps in templ
// components that need to redraw their own marker on focus changes.
func (b *SelectBase) FocusedState() *tui.State[bool] {
	return b.focused
}

// Focus seeds the local marker and uses FocusTraversal to repair the app
// focus graph. Call this on a one-shot mount/update transition, not from
// Render or BindApp loops.
func (b *SelectBase) Focus(app *tui.App) {
	if b.focused != nil {
		b.focused.Set(true)
	}
	FocusRefByTraversal(app, b.Ref)
}

// App returns the cached app handle, or nil if not bound.
func (b *SelectBase) App() *tui.App {
	return b.app
}

// BindApp wires the component's state to the app.
func (b *SelectBase) BindApp(app *tui.App) {
	if b.focused != nil {
		b.focused.BindApp(app)
	}
	b.app = app
}

// UnbindApp releases the cached app handle when the row leaves the tree.
func (b *SelectBase) UnbindApp() {
	b.app = nil
}

// Traversal returns the canonical Up/Down keymap for any SelectBase-derived
// component. Components should append this to their KeyMap after activation.
// WithActivation returns the canonical keymap for a SelectBase-derived
// component: Enter/Space activation plus Up/Down traversal. Components
// should return this from KeyMap, passing their activation handler.
func (b *SelectBase) WithActivation(fn func(tui.KeyEvent)) tui.KeyMap {
	return append(ActivateFocused(fn), b.Traversal()...)
}

func (b *SelectBase) WithTraversal(parts ...tui.KeyMap) tui.KeyMap {
	traversal := b.Traversal()

	n := len(traversal)
	for _, p := range parts {
		n += len(p)
	}

	out := make(tui.KeyMap, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	out = append(out, traversal...)
	return out
}

func (b *SelectBase) Traversal() tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyDown, MoveNext),
		tui.OnFocused(tui.KeyUp, MovePrev),
	}
}
