package ui

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui/interaction"
)

// SelectableRow is a selectable action row with focus markers and activation.
// Sections mount this via app.Mount() so it participates in the go-tui focus
// graph and has KeyMap-based activation.
//
// Escape is handled only when the caller supplies OnEscape for row-local opened
// state such as an inline confirmation. Plain rows deliberately ignore Escape.
type SelectableRow struct {
	target   *interaction.Selectable
	Label    string
	Value    string
	Action   string
	Activate func()
	OnEscape func()
	// AutoFocus seeds the row as selected on mount, or on the first transition
	// from false to true on an already-mounted row.
	AutoFocus bool
}

// NewSelectableRow builds a mountable selectable row.
func NewSelectableRow(id, label, value, action string, activate func()) *SelectableRow {
	row := &SelectableRow{
		Label:    label,
		Value:    value,
		Action:   action,
		Activate: activate,
	}
	row.target = interaction.NewSelectable(row.propsWithID(id))
	return row
}

func (r *SelectableRow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*SelectableRow)
	if !ok {
		return
	}

	r.Label = f.Label
	r.Value = f.Value
	r.Action = f.Action
	r.Activate = f.Activate
	r.OnEscape = f.OnEscape
	r.AutoFocus = f.AutoFocus

	r.target.Update(r.props())
}

// Arrow returns the selected-row marker. AutoFocus is a mount/update seed only;
// it must not keep painting the marker after real focus moves elsewhere.
func (r *SelectableRow) Arrow() string {
	return r.target.Marker()
}

func (r *SelectableRow) Init() func() {
	r.target.Update(r.props())
	if !r.AutoFocus || r.target.IsFocused() {
		return nil
	}

	r.target.Init()
	return nil
}

func (r *SelectableRow) Render(app *tui.App) *tui.Element {
	// Render stays pure; one-shot autofocus seeding happens in Init or
	// UpdateProps.
	r.target.SetRenderProps(r.props())
	opts := append(r.target.ShellOptions(), tui.WithOnActivate(r.Activate))
	root := ActionRow(r.Arrow(), r.Label, r.Value, r.Action, opts...)
	r.target.BindElement(root)
	return root
}

// KeyMap returns the keyboard bindings for activation, optional local Escape,
// and traversal.
func (r *SelectableRow) KeyMap() tui.KeyMap {
	return r.target.KeyMap()
}

func (r *SelectableRow) BindApp(app *tui.App) { r.target.BindApp(app) }

func (r *SelectableRow) UnbindApp() { r.target.UnbindApp() }

func (r *SelectableRow) IsFocused() bool { return r.target.IsFocused() }

func (r *SelectableRow) Focus(app *tui.App) { r.target.Focus(app) }

func (r *SelectableRow) Blur() { r.target.Blur() }

func (r *SelectableRow) Ref() *tui.Ref { return r.target.Ref() }

func (r *SelectableRow) props() interaction.SelectableProps {
	return r.propsWithID(r.target.Props().ID)
}

func (r *SelectableRow) propsWithID(id string) interaction.SelectableProps {
	props := interaction.SelectableProps{
		ID:        id,
		Label:     r.Label,
		Value:     r.Value,
		Action:    r.Action,
		AutoFocus: r.AutoFocus,
		OnActivate: func(interaction.Context) {
			if r.Activate != nil {
				r.Activate()
			}
		},
	}
	if r.OnEscape != nil {
		props.OnEscape = func(interaction.Context) {
			r.OnEscape()
		}
	}
	return props
}

var (
	_ tui.Component    = (*SelectableRow)(nil)
	_ tui.KeyListener  = (*SelectableRow)(nil)
	_ tui.AppBinder    = (*SelectableRow)(nil)
	_ tui.Initializer  = (*SelectableRow)(nil)
	_ tui.PropsUpdater = (*SelectableRow)(nil)
)
