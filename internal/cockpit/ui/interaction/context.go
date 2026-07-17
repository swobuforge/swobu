package interaction

import tui "github.com/grindlemire/go-tui"

// Context is the event object passed through the interaction grammar.
//
// It deliberately carries the go-tui app and key event without exposing refs,
// focus traversal, or dispatch priority to product packages.
type Context struct {
	App      *tui.App
	KeyEvent tui.KeyEvent
}

func contextFromEvent(event tui.KeyEvent) Context {
	return Context{App: event.App(), KeyEvent: event}
}
