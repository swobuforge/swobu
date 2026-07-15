package ui

import tui "github.com/grindlemire/go-tui"

// ActivateFocused returns the shared Enter/Space key grammar for focused rows.
func ActivateFocused(fn func(tui.KeyEvent)) tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyEnter, fn),
		tui.OnFocused(tui.Rune(' '), fn),
	}
}

// MoveNext advances focus to the next focusable element.
func MoveNext(ke tui.KeyEvent) {
	ke.App().FocusNext()
}

// MovePrev moves focus to the previous focusable element.
func MovePrev(ke tui.KeyEvent) {
	ke.App().FocusPrev()
}
