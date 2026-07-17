package interaction

import tui "github.com/grindlemire/go-tui"

// FocusRefByTraversal focuses ref using go-tui's public focus traversal.
//
// go-tui v0.17 does not expose direct focus-by-ref. The interaction package is
// the only Cockpit layer allowed to carry this workaround, and only for
// mount/update focus repair.
func FocusRefByTraversal(app *tui.App, ref *tui.Ref) {
	if app == nil || ref == nil {
		return
	}
	app.QueueUpdate(func() {
		target := ref.El()
		if target == nil || app.Focused() == target {
			return
		}
		for range 512 {
			app.FocusNext()
			if app.Focused() == target {
				return
			}
		}
	})
}

func focusRefByTraversalNow(app *tui.App, ref *tui.Ref) {
	if app == nil || ref == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	target := ref.El()
	if target == nil || app.Focused() == target {
		return
	}
	app.BlurFocused()
	for range 512 {
		app.FocusNext()
		if app.Focused() == target {
			return
		}
	}
}
