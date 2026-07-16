package ui

import tui "github.com/grindlemire/go-tui"

// FocusTraversal implements best-effort focus relocation via public tab-order
// traversal. go-tui does not expose a direct focus-by-ref API, so components
// that need to restore focus after a mount/update transition must walk the
// focus ring. This is a workaround layer, not normal application logic.
//
// The repair runs through app.QueueUpdate, so callers and test harnesses must
// drain queued updates before expecting the repaired focus to be visible.
//
// Callers should prefer the dispatched helpers (FocusRefByTraversal,
// focusFirstByTraversal) over the raw internal routines.

// FocusRefByTraversal focuses the element referenced by ref using only public
// tab-order traversal. It is best-effort: the target must still exist in the
// current focus ring.
func FocusRefByTraversal(app *tui.App, ref *tui.Ref) {
	if app == nil || ref == nil {
		return
	}

	app.QueueUpdate(func() {
		if el := ref.El(); el != nil {
			focusElementByTraversal(app, el)
		}
	})
}

// focusFirstByTraversal focuses the first resolvable element among refs.
// Unresolved refs are skipped silently.
func focusFirstByTraversal(app *tui.App, refs ...*tui.Ref) {
	if app == nil {
		return
	}

	app.QueueUpdate(func() {
		for _, ref := range refs {
			if ref == nil {
				continue
			}

			if el := ref.El(); el != nil && focusElementByTraversal(app, el) {
				return
			}
		}
	})
}

func focusElementByTraversal(app *tui.App, target tui.Focusable) bool {
	if app == nil || target == nil {
		return false
	}

	root := app.Root()
	if root == nil {
		return false
	}

	focusables := tabStopsInOrder(root)
	if len(focusables) == 0 {
		return false
	}

	targetIdx := focusableIndex(focusables, target)
	if targetIdx < 0 {
		return false
	}

	current := app.Focused()
	if current == target {
		return true
	}

	currentIdx := focusableIndex(focusables, current)
	prevSteps, nextSteps := traversalSteps(len(focusables), currentIdx, targetIdx)
	if nextSteps < prevSteps {
		for i := 0; i < nextSteps; i++ {
			app.FocusNext()
		}
	} else {
		for i := 0; i < prevSteps; i++ {
			app.FocusPrev()
		}
	}

	return app.Focused() == target
}

func tabStopsInOrder(root *tui.Element) []tui.Focusable {
	if root == nil {
		return nil
	}

	focusables := make([]tui.Focusable, 0)
	root.WalkFocusables(func(f tui.Focusable) {
		focusables = append(focusables, f)
	})
	return focusables
}

func focusableIndex(focusables []tui.Focusable, target tui.Focusable) int {
	for i, focusable := range focusables {
		if focusable == target {
			return i
		}
	}
	return -1
}

func traversalSteps(count, currentIdx, targetIdx int) (prevSteps, nextSteps int) {
	if count <= 0 {
		return 0, 0
	}

	if currentIdx < 0 {
		prevSteps = count - targetIdx
		nextSteps = targetIdx + 1
		return prevSteps, nextSteps
	}

	prevSteps = (currentIdx - targetIdx + count) % count
	nextSteps = (targetIdx - currentIdx + count) % count
	return prevSteps, nextSteps
}
