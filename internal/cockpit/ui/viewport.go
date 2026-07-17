package ui

import tui "github.com/grindlemire/go-tui"

// Viewport owns a scrollable viewport's ref, scroll offset, page movement, and
// optional focused-selection follow behavior.
type Viewport struct {
	Ref           *tui.Ref
	ScrollY       *tui.State[int]
	FollowFocused bool
	MarginRows    int
}

// NewViewport creates a scrollable viewport with a ref and scroll state.
func NewViewport() *Viewport {
	return &Viewport{
		Ref:     tui.NewRef(),
		ScrollY: tui.NewState(0),
	}
}

// BindApp wires viewport state to go-tui.
func (v *Viewport) BindApp(app *tui.App) {
	if v == nil {
		return
	}
	v.ensure()
	v.ScrollY.BindApp(app)
}

// Reset returns the viewport to the top.
func (v *Viewport) Reset() {
	if v == nil {
		return
	}
	v.ensure()
	v.ScrollY.Set(0)
}

// Page scrolls by one viewport height in the requested direction.
func (v *Viewport) Page(delta int) {
	if v == nil {
		return
	}
	v.ensure()
	body := v.Ref.El()
	if body == nil {
		return
	}
	_, viewportHeight := body.ViewportSize()
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	_, maxY := body.MaxScroll()
	next := v.ScrollY.Get() + delta*viewportHeight
	v.setClamped(next, maxY)
}

// FollowFocusedSelection scrolls just enough to keep the focused element inside
// the viewport margin.
func (v *Viewport) FollowFocusedSelection(app *tui.App) {
	if v == nil || !v.FollowFocused || app == nil {
		return
	}
	v.ensure()
	viewport := v.Ref.El()
	if viewport == nil {
		return
	}
	focused, ok := app.Focused().(*tui.Element)
	if !ok || focused == nil {
		return
	}
	if !elementDescendsFrom(focused, viewport) {
		return
	}
	next, changed := v.offsetForFocusedElement(viewport, focused)
	if changed {
		v.ScrollY.Set(next)
	}
}

// WithViewportFollow installs the viewport's focused-selection follow hook.
func WithViewportFollow(viewport *Viewport, app func() *tui.App) tui.AppOption {
	return tui.WithPostRenderHook(func() {
		if app == nil {
			return
		}
		viewport.FollowFocusedSelection(app())
	})
}

func (v *Viewport) ensure() {
	if v.Ref == nil {
		v.Ref = tui.NewRef()
	}
	if v.ScrollY == nil {
		v.ScrollY = tui.NewState(0)
	}
}

func (v *Viewport) setClamped(next, maxY int) {
	if next < 0 {
		next = 0
	}
	if next > maxY {
		next = maxY
	}
	v.ScrollY.Set(next)
}

func elementDescendsFrom(child *tui.Element, ancestor *tui.Element) bool {
	for el := child; el != nil; el = el.Parent() {
		if el == ancestor {
			return true
		}
	}
	return false
}

func (v *Viewport) offsetForFocusedElement(viewport *tui.Element, selected *tui.Element) (int, bool) {
	current := v.ScrollY.Get()
	if viewport == nil || selected == nil {
		return current, false
	}
	_, viewportHeight := viewport.ViewportSize()
	if viewportHeight <= 0 {
		return current, false
	}
	margin := v.MarginRows
	if margin < 0 {
		margin = 0
	}
	if viewportHeight <= margin*2 {
		margin = 0
	}

	focusedRect := selected.Rect()
	anchorHeight := focusedRect.Height
	if anchorHeight <= 0 {
		anchorHeight = 1
	}
	relativeY := focusedRect.Y
	next := current
	if relativeY < current+margin {
		next = relativeY - margin
	} else if relativeY+anchorHeight > current+viewportHeight-margin {
		next = relativeY + anchorHeight - viewportHeight + margin
	}

	_, maxY := viewport.MaxScroll()
	if next < 0 {
		next = 0
	}
	if next > maxY {
		next = maxY
	}
	return next, next != current
}
