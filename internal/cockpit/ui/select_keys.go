package ui

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui/interaction"
)

// Shared selected-row grammar for row markers.
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

// ActivateSelected returns the shared Enter/Space key grammar for the currently
// selected component. Selection is implemented with go-tui focus, but callers
// should treat this as operator selection, not a focus-manager API.
func ActivateSelected(fn func(tui.KeyEvent)) tui.KeyMap {
	return interaction.ActivateSelected(func(ctx interaction.Context) { fn(ctx.KeyEvent) })
}

// ActivateCurrentSelection dispatches Enter to the mounted element currently
// selected by Up/Down traversal.
func ActivateCurrentSelection(event tui.KeyEvent) {
	app := event.App()
	if app == nil || app.Focused() == nil {
		return
	}
	if element, ok := app.Focused().(*tui.Element); ok {
		element.Activate()
	}
}

// SelectNext moves operator selection to the next selectable element.
func SelectNext(ke tui.KeyEvent) {
	interaction.SelectNext(ke)
}

// SelectPrevious moves operator selection to the previous selectable element.
func SelectPrevious(ke tui.KeyEvent) {
	interaction.SelectPrevious(ke)
}

// BackScope returns the active-scope Escape grammar for a feature that owns an
// entered semantic state. While active, Escape backs out of this scope instead
// of bubbling to a page/root fallback such as app quit. The scope declares only
// whether it is entered and how to back out; it does not coordinate with child
// rows or parent surfaces.
func BackScope(active func() bool, back func()) tui.KeyMap {
	return interaction.BackScope(active, func(interaction.Context) {
		if back != nil {
			back()
		}
	})
}
