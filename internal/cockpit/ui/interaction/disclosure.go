package interaction

import tui "github.com/grindlemire/go-tui"

// DisclosureProps describes a focusable header that opens or closes a region.
type DisclosureProps struct {
	ID        string
	Label     string
	Expanded  *tui.State[bool]
	AutoFocus bool

	OnExpand   func(Context)
	OnCollapse func(Context)
}

// Disclosure is the interaction grammar for a collapsible focusable header.
//
// Enter and Space toggle the expanded state. Escape collapses only when the
// disclosure is currently expanded; otherwise it deliberately omits its Escape
// binding so broader backout owners can act.
type Disclosure struct {
	selectable *Selectable
	props      DisclosureProps
}

// NewDisclosure creates a disclosure grammar component.
func NewDisclosure(props DisclosureProps) *Disclosure {
	d := &Disclosure{props: props}
	d.selectable = NewSelectable(SelectableProps{
		ID:         props.ID,
		Label:      props.Label,
		OnActivate: d.toggle,
		OnEscape:   d.escape,
	})
	return d
}

// BindApp wires state to the app.
func (d *Disclosure) BindApp(app *tui.App) {
	d.selectable.BindApp(app)
	if d.props.Expanded != nil {
		d.props.Expanded.BindApp(app)
	}
}

// UnbindApp releases app state.
func (d *Disclosure) UnbindApp() { d.selectable.UnbindApp() }

// UpdateProps updates display/callback props while preserving focus state.
func (d *Disclosure) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*Disclosure)
	if !ok {
		return
	}
	d.Update(f.props)
}

// Update replaces display and callback props while preserving focus state.
func (d *Disclosure) Update(props DisclosureProps) {
	d.props = props
	d.selectable.Update(d.selectableProps(true))
}

// SetRenderProps refreshes display and callback props without lifecycle focus
// repair.
func (d *Disclosure) SetRenderProps(props DisclosureProps) {
	d.props = props
	d.selectable.SetRenderProps(d.selectableProps(true))
}

// Init seeds one-shot selection after the disclosure shell mounts.
func (d *Disclosure) Init() func() { return d.selectable.Init() }

// IsFocused satisfies go-tui's focus-gated dispatch contract.
func (d *Disclosure) IsFocused() bool { return d.selectable.IsFocused() }

// Marker returns the shared focus marker.
func (d *Disclosure) Marker() string { return d.selectable.Marker() }

// Props returns a copy of the current disclosure props.
func (d *Disclosure) Props() DisclosureProps { return d.props }

// ShellOptions returns the options required for a custom disclosure shell.
func (d *Disclosure) ShellOptions() []tui.Option { return d.selectable.ShellOptions() }

// BindElement records a custom row shell as the disclosure's focus element.
func (d *Disclosure) BindElement(el *tui.Element) { d.selectable.BindElement(el) }

// Render returns the default minimal shell.
func (d *Disclosure) Render(app *tui.App) *tui.Element { return d.selectable.Render(app) }

// KeyMap returns disclosure activation, traversal, and Escape only while the
// disclosure has local expanded state to close.
func (d *Disclosure) KeyMap() tui.KeyMap {
	props := d.props
	if props.Expanded != nil && props.Expanded.Get() {
		return d.selectable.KeyMap()
	}
	d.selectable.SetRenderProps(d.selectableProps(false))
	return d.selectable.KeyMap()
}

func (d *Disclosure) selectableProps(withEscape bool) SelectableProps {
	props := SelectableProps{
		ID:         d.props.ID,
		Label:      d.props.Label,
		AutoFocus:  d.props.AutoFocus,
		OnActivate: d.toggle,
	}
	if withEscape {
		props.OnEscape = d.escape
	}
	return props
}

func (d *Disclosure) toggle(ctx Context) {
	if d.props.Expanded == nil {
		return
	}
	next := !d.props.Expanded.Get()
	d.props.Expanded.Set(next)
	if next {
		if d.props.OnExpand != nil {
			d.props.OnExpand(ctx)
		}
		return
	}
	if d.props.OnCollapse != nil {
		d.props.OnCollapse(ctx)
	}
}

func (d *Disclosure) escape(ctx Context) {
	if d.props.Expanded == nil || !d.props.Expanded.Get() {
		return
	}
	d.props.Expanded.Set(false)
	if d.props.OnCollapse != nil {
		d.props.OnCollapse(ctx)
	}
}

var (
	_ tui.Component    = (*Disclosure)(nil)
	_ tui.KeyListener  = (*Disclosure)(nil)
	_ tui.AppBinder    = (*Disclosure)(nil)
	_ tui.Initializer  = (*Disclosure)(nil)
	_ tui.PropsUpdater = (*Disclosure)(nil)
)
