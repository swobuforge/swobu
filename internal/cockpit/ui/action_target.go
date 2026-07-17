package ui

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui/interaction"
)

// ActionTarget is the product-facing behavior carrier for a custom selectable
// action shell.
//
// Use SelectableRow when the standard row layout fits. Use ActionTarget when a
// ui or section component owns a custom shell shape but still wants the shared
// Cockpit interaction grammar: Enter/Space activation, Up/Down selection, and
// one go-tui focus authority.
type ActionTarget struct {
	target *interaction.Selectable
}

// NewActionTarget creates an action target with a stable mount/debug ID.
func NewActionTarget(id string, activate func()) *ActionTarget {
	t := &ActionTarget{}
	t.target = interaction.NewSelectable(t.propsWithID(id, activate))
	return t
}

// BindApp wires target state to the app.
func (t *ActionTarget) BindApp(app *tui.App) { t.target.BindApp(app) }

// UnbindApp releases the app handle.
func (t *ActionTarget) UnbindApp() { t.target.UnbindApp() }

// IsFocused satisfies go-tui focused dispatch.
func (t *ActionTarget) IsFocused() bool { return t.target.IsFocused() }

// Focus moves selection to this target after a mount/update transition.
func (t *ActionTarget) Focus(app *tui.App) { t.target.Focus(app) }

// Blur clears the local selected marker.
func (t *ActionTarget) Blur() { t.target.Blur() }

// Marker returns the shared selected-row marker.
func (t *ActionTarget) Marker() string { return t.target.Marker() }

// FocusedState exposes marker state to generated render deps.
func (t *ActionTarget) FocusedState() *tui.State[bool] { return t.target.FocusedState() }

// Ref returns the custom shell ref for GSX-owned row shapes.
func (t *ActionTarget) Ref() *tui.Ref { return t.target.Ref() }

// OnFocus records shell focus for GSX-owned row shapes.
func (t *ActionTarget) OnFocus(el *tui.Element) { t.target.Cell.OnFocus(el) }

// OnBlur records shell blur for GSX-owned row shapes.
func (t *ActionTarget) OnBlur(el *tui.Element) { t.target.Cell.OnBlur(el) }

// ShellOptions returns structural go-tui options for the custom shell element.
func (t *ActionTarget) ShellOptions() []tui.Option {
	return t.target.ShellOptions()
}

// BindElement records the custom shell element as the target.
func (t *ActionTarget) BindElement(el *tui.Element) { t.target.BindElement(el) }

// KeyMap returns activation, optional Escape, and traversal bindings.
func (t *ActionTarget) KeyMap(activate func(), escape func()) tui.KeyMap {
	t.target.Update(t.propsWithID(t.target.Props().ID, activate, escape))
	return t.target.KeyMap()
}

func (t *ActionTarget) propsWithID(id string, activate func(), escape ...func()) interaction.SelectableProps {
	props := interaction.SelectableProps{
		ID: id,
		OnActivate: func(interaction.Context) {
			if activate != nil {
				activate()
			}
		},
	}
	if len(escape) > 0 && escape[0] != nil {
		props.OnEscape = func(interaction.Context) {
			escape[0]()
		}
	}
	return props
}
