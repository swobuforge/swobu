package interaction

import tui "github.com/grindlemire/go-tui"

// SelectableProps describes one focusable action target.
type SelectableProps struct {
	ID        string
	Label     string
	Value     string
	Action    string
	AutoFocus bool

	OnActivate func(Context)
	OnEscape   func(Context)
}

// Selectable is the grammar component for a focusable action target.
//
// It does not prescribe row layout. Parent ui primitives render labels, values,
// and actions in their own design-system shape while delegating focus state and
// key ownership here.
type Selectable struct {
	Cell FocusCell

	props SelectableProps
}

// NewSelectable creates a selectable grammar component.
func NewSelectable(props SelectableProps) *Selectable {
	return &Selectable{
		Cell:  NewFocusCell(props.ID),
		props: props,
	}
}

// BindApp wires the underlying focus cell.
func (s *Selectable) BindApp(app *tui.App) { s.Cell.BindApp(app) }

// UnbindApp releases the app handle.
func (s *Selectable) UnbindApp() { s.Cell.UnbindApp() }

// UpdateProps updates the selectable while preserving focus marker state.
func (s *Selectable) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*Selectable)
	if !ok {
		return
	}
	s.Update(f.props)
}

// Update replaces display and callback props while preserving focus state.
func (s *Selectable) Update(props SelectableProps) {
	wasAutoFocus := s.props.AutoFocus
	s.props = props
	if !wasAutoFocus && s.props.AutoFocus && !s.Cell.IsFocused() {
		s.Cell.Focus(s.Cell.app)
	}
}

// SetRenderProps refreshes display and callback props from render helpers
// without performing lifecycle focus repair.
func (s *Selectable) SetRenderProps(props SelectableProps) {
	s.props = props
}

// Init seeds one-shot autofocus.
func (s *Selectable) Init() func() {
	if s.props.AutoFocus && !s.Cell.IsFocused() {
		s.Cell.Focus(s.Cell.app)
	}
	return nil
}

// IsFocused satisfies go-tui's focus-gated dispatch contract.
func (s *Selectable) IsFocused() bool { return s.Cell.IsFocused() }

// Marker returns the shared focus marker for layout wrappers.
func (s *Selectable) Marker() string { return s.Cell.Marker() }

// MarkerWithActiveDescendant marks the shell active while a local non-focusable
// child surface, such as an inline editor, owns the current interaction.
func (s *Selectable) MarkerWithActiveDescendant(activeDescendant bool) string {
	if s.Cell.IsFocused() || activeDescendant {
		return ">"
	}
	return " "
}

// FocusedState exposes the marker state to templ-generated render deps.
func (s *Selectable) FocusedState() *tui.State[bool] { return s.Cell.FocusedState() }

// Focus repairs focus to this selectable after a mount/update transition.
func (s *Selectable) Focus(app *tui.App) { s.Cell.Focus(app) }

// FocusNow repairs focus immediately after a render has resolved the shell ref.
func (s *Selectable) FocusNow(app *tui.App) { focusRefByTraversalNow(app, s.Cell.Ref) }

// Blur clears the local marker when a parent intentionally moves selection
// away from a cached row.
func (s *Selectable) Blur() {
	if state := s.Cell.FocusedState(); state != nil {
		state.Set(false)
	}
}

// App returns the currently bound app, if mounted.
func (s *Selectable) App() *tui.App { return s.Cell.app }

// Ref returns the mounted shell reference.
func (s *Selectable) Ref() *tui.Ref { return s.Cell.Ref }

// Props returns a copy of the current display props.
func (s *Selectable) Props() SelectableProps { return s.props }

// Render returns a minimal focusable shell. Parent ui components can use
// ShellOptions when they need to render richer row structure around the shell.
func (s *Selectable) Render(*tui.App) *tui.Element {
	el := tui.New(s.ShellOptions()...)
	if text := s.shellText(); text != "" {
		el.SetText(text)
	}
	if s.Cell.Ref != nil {
		s.Cell.Ref.Set(el)
	}
	return el
}

// ShellOptions returns the options required for a custom row shell to satisfy
// the interaction grammar.
func (s *Selectable) ShellOptions() []tui.Option {
	opts := []tui.Option{
		tui.WithFocusable(true),
		tui.WithOnFocus(s.Cell.OnFocus),
		tui.WithOnBlur(s.Cell.OnBlur),
	}
	if s.props.AutoFocus {
		opts = append(opts, tui.WithAutoFocus(true))
	}
	return opts
}

// BindElement records a custom row shell as the selectable's focus element.
func (s *Selectable) BindElement(el *tui.Element) {
	if s.Cell.Ref != nil {
		s.Cell.Ref.Set(el)
	}
}

// KeyMap owns activation, optional Escape, and selection traversal.
func (s *Selectable) KeyMap() tui.KeyMap {
	local := ActivateSelected(func(ctx Context) {
		if s.props.OnActivate != nil {
			s.props.OnActivate(ctx)
		}
	})
	if s.props.OnEscape != nil {
		local = append(local, tui.OnFocused(tui.KeyEscape, func(event tui.KeyEvent) {
			s.props.OnEscape(contextFromEvent(event))
		}))
	}
	return WithTraversal(local)
}

func (s *Selectable) shellText() string {
	if s.props.Label == "" && s.props.Value == "" && s.props.Action == "" {
		return ""
	}
	return s.props.Label + " " + s.props.Value + " " + s.props.Action
}

var (
	_ tui.Component    = (*Selectable)(nil)
	_ tui.KeyListener  = (*Selectable)(nil)
	_ tui.AppBinder    = (*Selectable)(nil)
	_ tui.Initializer  = (*Selectable)(nil)
	_ tui.PropsUpdater = (*Selectable)(nil)
)
