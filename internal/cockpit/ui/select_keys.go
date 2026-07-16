package ui

import tui "github.com/grindlemire/go-tui"

// ActivateFocused returns the shared Enter/Space key grammar for any focused
// component that owns a local activation behavior.
func ActivateFocused(fn func(tui.KeyEvent)) tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyEnter, fn),
		tui.OnFocused(tui.Rune(' '), fn),
	}
}

// ActivateFocusedElement dispatches Enter to the currently focused mounted
// component. Surfaces use this as the canonical fallback when the product action
// is "activate whatever row/control currently owns focus".
func ActivateFocusedElement(event tui.KeyEvent) {
	app := event.App()
	if app == nil || app.Focused() == nil {
		return
	}
	if element, ok := app.Focused().(*tui.Element); ok {
		element.Activate()
	}
}

// MoveNext advances focus to the next focusable element.
func MoveNext(ke tui.KeyEvent) {
	if app := ke.App(); app != nil {
		app.FocusNext()
	}
}

// MovePrev moves focus to the previous focusable element.
func MovePrev(ke tui.KeyEvent) {
	if app := ke.App(); app != nil {
		app.FocusPrev()
	}
}
