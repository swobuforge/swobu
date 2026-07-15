package ui

import tui "github.com/grindlemire/go-tui"

// SelectBase provides focus-aware identity and state for selectable cockpit
// rows. It embeds into struct components that participate in the go-tui focus
// graph and need best-effort traversal-based focus repair after local render
// changes.
type SelectBase struct {
	ID      string
	Ref     *tui.Ref
	focused *tui.State[bool]
}

// NewSelectBase creates a new SelectBase with the given stable ID.
func NewSelectBase(id string) SelectBase {
	return SelectBase{
		ID:      id,
		Ref:     tui.NewRef(),
		focused: tui.NewState(false),
	}
}

// OnFocus marks the base as focused.
func (b *SelectBase) OnFocus(el *tui.Element) {
	b.focused.Set(true)
}

// OnBlur marks the base as unfocused.
func (b *SelectBase) OnBlur(el *tui.Element) {
	b.focused.Set(false)
}

// IsFocused returns true if the referenced go-tui element is focused,
// falling back to the local cache if the ref is not resolved.
func (b *SelectBase) IsFocused() bool {
	if b.Ref != nil {
		if el := b.Ref.El(); el != nil {
			return el.IsFocused()
		}
	}
	return b.focused.Get()
}

// Arrow returns the shared selection marker for the current focus state.
func (b *SelectBase) Arrow() string {
	if b.IsFocused() {
		return SelectArrowFocused
	}
	return SelectArrowBlurred
}

// Focus moves focus back to the referenced element using public traversal.
func (b *SelectBase) Focus(app *tui.App) {
	FocusRefByTraversal(app, b.Ref)
}

// BindApp wires the component's state to the app.
func (b *SelectBase) BindApp(app *tui.App) {
	if b.focused != nil {
		b.focused.BindApp(app)
	}
}

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
