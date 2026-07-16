package cockpit

import tui "github.com/grindlemire/go-tui"

// focusedTextEditor returns true when the currently focused go-tui element is
// a text-editing control (Input or TextArea). It is used by root key handlers
// that must short-circuit when the operator is typing rather than navigating.
//
// This helper must live outside the root GSX file because the root source rule
// rejects direct use of app.Focused() in generated output.
func focusedTextEditor(event tui.KeyEvent) bool {
	app := event.App()
	if app == nil {
		return false
	}
	switch app.Focused().(type) {
	case *tui.Input, *tui.TextArea:
		return true
	case *tui.Element:
		focused := app.Focused().(*tui.Element)
		switch focused.Component().(type) {
		case *tui.Input, *tui.TextArea:
			return true
		}
	default:
		return false
	}
	return false
}
