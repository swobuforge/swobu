package ui

import tui "github.com/grindlemire/go-tui"

// ControlEvent is the event shape passed to FocusableControl lifecycle
// callbacks. It carries the app reference so callbacks can inspect focus
// state or queue further updates.
type ControlEvent struct {
	App      *tui.App
	KeyEvent tui.KeyEvent
}

// FocusableControl is the canonical cockpit interaction primitive.
//
// It owns the full lifecycle of an interactive control:
//
//   Focus/Blur  — shell receives/loses arrow traversal focus
//   Activate    — Enter/Space on focused shell opens the control
//   Enter       — focus moves into the opened interior (optional callback)
//   Exit        — Escape closes the control
//
// A control has two regions:
//
//   shell    — the focusable row/control itself (always mounted)
//   interior — optional child subtree mounted after activation
//
// Rules:
//
//   * The control shell is a single focusable element; it does NOT wrap
//     children in a container div. The control is the shell marker.
//   * OnFocused(KeyEscape) catches Escape only when the control or one
//     of its descendants has focus. This avoids Normal Pass conflicts
//     with page-level OnStop handlers. Children must NOT use
//     OnFocused(KeyEscape) or they will block the scope owner.
//   * OnActivate fires only when the shell itself has direct focus
//     (isFocusedShell == true).
//   * After OnExit, focus returns to ReturnFocus if set, otherwise to Ref.
//
// Do not build a second UI framework around this type. Use it for modal
// workflows, inline editors, expandable rows, pickers, and any control
// that the operator can enter and must be able to exit.
type FocusableControl struct {
	// ID is a stable identifier used for debug and mount keys.
	ID string

	// Ref holds the go-tui element for this control's shell.
	// Callers should not resolve the ref directly; use the FocusedShell/
	// FocusWithin helpers instead.
	Ref *tui.Ref

	// Open signals whether the control's interior is mounted.
	// The control watches this state; callers mutate it in OnActivate/OnExit.
	Open *tui.State[bool]

	// ShellText is the optional text rendered inside the shell element.
	// If empty, the shell is focusable but invisible (callers render
	// their own label/value/action columns around it).
	ShellText *tui.State[string]

	// OnFocus fires when the shell gains direct focus from arrow traversal.
	OnFocus func(ControlEvent)

	// OnBlur fires when the shell loses direct focus.
	OnBlur func(ControlEvent)

	// OnActivate fires when Enter or Space is pressed while the shell
	// has direct focus. The callback typically sets Open to true.
	OnActivate func(ControlEvent)

	// OnEnter is optional. It fires when focus moves from the shell
	// into the opened interior. Most components do not need this.
	OnEnter func(ControlEvent)

	// OnExit fires when Escape is dispatched while the control is open
	// and focus is anywhere within it (shell or interior). The callback
	// typically sets Open to false.
	OnExit func(ControlEvent)

	// ReturnFocus, if set, receives focus after OnExit. If nil, Ref is
	// used instead.
	ReturnFocus *tui.Ref

	// AutoFocus gives the shell autoFocus when the control mounts.
	AutoFocus bool

	app     *tui.App
	focused *tui.State[bool]
}

// NewFocusableControl creates a FocusableControl with the given ID.
func NewFocusableControl(id string) *FocusableControl {
	return &FocusableControl{
		ID:      id,
		Ref:     tui.NewRef(),
		Open:    tui.NewState(false),
		focused: tui.NewState(false),
	}
}

// --- Lifecycle ---

// BindApp wires internal state to the app.
func (c *FocusableControl) BindApp(app *tui.App) {
	c.app = app
	c.Open.BindApp(app)
	c.focused.BindApp(app)
	if c.ShellText != nil {
		c.ShellText.BindApp(app)
	}
}

// UnbindApp releases the app handle.
func (c *FocusableControl) UnbindApp() {
	c.app = nil
}

// Init returns a cleanup function; called by the mount system.
func (c *FocusableControl) Init() func() {
	return nil
}

// --- Render ---

// Render returns the focusable shell element. The control does NOT render
// interior children; the caller mounts them as siblings or children of a
// parent container. This keeps the control a thin marker, not a layout wrapper.
func (c *FocusableControl) Render(app *tui.App) *tui.Element {
	opts := []tui.Option{
		tui.WithFocusable(true),
	}
	if c.AutoFocus {
		opts = append(opts, tui.WithAutoFocus(true))
	}
	if c.ShellText != nil && c.ShellText.Get() != "" {
		opts = append(opts, tui.WithText(c.ShellText.Get()))
	}

	el := tui.New(opts...)
	if c.Ref != nil {
		c.Ref.Set(el)
	}
	el.SetOnFocus(func(*tui.Element) {
		c.focused.Set(true)
		if c.OnFocus != nil {
			c.OnFocus(ControlEvent{App: app})
		}
	})
	el.SetOnBlur(func(*tui.Element) {
		c.focused.Set(false)
		if c.OnBlur != nil {
			c.OnBlur(ControlEvent{App: app})
		}
	})
	return el
}

// --- KeyMap ---

// KeyMap returns the control's bindings. All use OnFocused so they run in the
// Priority Pass and never conflict with page-level OnStop handlers.
func (c *FocusableControl) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyEnter, func(e tui.KeyEvent) { c.activate(e) }),
		tui.OnFocused(tui.KeyRune, func(e tui.KeyEvent) {
			if e.Rune == ' ' {
				c.activate(e)
			}
		}),
		tui.OnFocused(tui.KeyEscape, func(e tui.KeyEvent) { c.exit(e) }),
	}
}

func (c *FocusableControl) activate(e tui.KeyEvent) {
	if !c.FocusedShell() {
		return
	}
	if c.OnActivate != nil {
		c.OnActivate(ControlEvent{App: e.App(), KeyEvent: e})
	}
}

func (c *FocusableControl) exit(e tui.KeyEvent) {
	if !c.Open.Get() {
		return
	}
	if c.OnExit != nil {
		c.OnExit(ControlEvent{App: e.App(), KeyEvent: e})
	}
	c.restoreFocus()
}

// --- Derived state ---

// FocusedShell returns true when this control's shell element has direct focus.
func (c *FocusableControl) FocusedShell() bool {
	if c.app == nil {
		return c.focused.Get()
	}
	el := c.Ref.El()
	if el == nil {
		return c.focused.Get()
	}
	return c.app.Focused() == el
}

// FocusWithin returns true when focus is on the shell OR anywhere inside
// the shell's descendant tree.
//
// In practice, tree-order dispatch means inner open controls handle Escape
// before outer controls, so FocusWithin is not required for correct Exit
// behavior. It is exposed for callers that need visual state (e.g. highlight
// shell when a descendant is focused).
func (c *FocusableControl) FocusWithin() bool {
	if c.app == nil {
		return c.focused.Get()
	}
	el := c.Ref.El()
	if el == nil {
		return c.focused.Get()
	}
	focused := c.app.Focused()
	if focused == nil {
		return false
	}
	// If the focused element is the shell itself, we're within.
	if focused == el {
		return true
	}
	// Walk up from the focused element to see if we reach the shell.
	fe, ok := focused.(*tui.Element)
	if !ok {
		return false
	}
	for e := fe; e != nil; e = e.Parent() {
		if e == el {
			return true
		}
	}
	return false
}

// FocusInInterior returns true when focus is inside the shell's descendant
// tree but NOT on the shell itself.
func (c *FocusableControl) FocusInInterior() bool {
	if !c.FocusWithin() {
		return false
	}
	return !c.FocusedShell()
}

// --- Focus repair ---

func (c *FocusableControl) restoreFocus() {
	if c.app == nil {
		return
	}
	if c.ReturnFocus != nil {
		FocusRefByTraversal(c.app, c.ReturnFocus)
	} else {
		FocusRefByTraversal(c.app, c.Ref)
	}
}
