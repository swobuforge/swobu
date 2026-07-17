package interaction

import tui "github.com/grindlemire/go-tui"

// SelectNext moves Cockpit's semantic selection to the next go-tui focusable.
func SelectNext(event tui.KeyEvent) {
	if app := event.App(); app != nil {
		app.FocusNext()
	}
}

// SelectPrevious moves Cockpit's semantic selection to the previous go-tui
// focusable.
func SelectPrevious(event tui.KeyEvent) {
	if app := event.App(); app != nil {
		app.FocusPrev()
	}
}

// ActivateSelected returns the standard Enter/Space activation grammar.
func ActivateSelected(fn func(Context)) tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyEnter, func(event tui.KeyEvent) {
			fn(contextFromEvent(event))
		}),
		tui.OnFocused(tui.Rune(' '), func(event tui.KeyEvent) {
			fn(contextFromEvent(event))
		}),
	}
}

// Traversal returns the standard Up/Down semantic selection grammar.
func Traversal() tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.KeyDown, SelectNext),
		tui.OnFocused(tui.KeyUp, SelectPrevious),
	}
}

// WithTraversal appends standard selection traversal after local bindings so
// control-specific handlers keep priority for keys they own.
func WithTraversal(parts ...tui.KeyMap) tui.KeyMap {
	traversal := Traversal()
	n := len(traversal)
	for _, part := range parts {
		n += len(part)
	}
	out := make(tui.KeyMap, 0, n)
	for _, part := range parts {
		out = append(out, part...)
	}
	out = append(out, traversal...)
	return out
}

// BackScope returns the standard active-scope Escape grammar.
//
// When active, Escape belongs to the nearest entered semantic owner rather than
// bubbling to a page/root fallback such as app quit. This is intentionally a
// tiny scope concept: it says only whether the scope is entered and how to back
// out of it.
func BackScope(active func() bool, back func(Context)) tui.KeyMap {
	if active == nil || back == nil || !active() {
		return nil
	}
	return tui.KeyMap{
		tui.OnPreemptStop(tui.KeyEscape, func(event tui.KeyEvent) {
			back(contextFromEvent(event))
		}),
	}
}
