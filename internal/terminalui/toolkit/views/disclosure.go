// Disclosure views compose parent and detail views into anchored sections.
package views

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// NewAnchoredDisclosureNode returns a core.Node that composes a parent node
// with detail nodes in a vertical stack, with each detail indented by 2
// columns. This is the canonical semantic entrypoint; the retained wrapper
// below is a legacy bridge.
func NewAnchoredDisclosureNode[E any](parent core.Node[E], details ...core.Node[E]) core.Node[E] {
	children := make([]core.Node[E], 0, len(details)+1)
	if !isEmptyNode(parent) {
		children = append(children, parent)
	}
	for _, d := range details {
		if !isEmptyNode(d) {
			children = append(children, d.Layout(core.Layout{
				Inset: core.Insets{Left: 2},
			}))
		}
	}
	return core.Stack[E](core.AxisVertical, children...)
}

func isEmptyNode[E any](n core.Node[E]) bool {
	return n.ContentValue().Text == "" && len(n.ChildrenValue()) == 0 && len(n.InteractionValue().Keymap) == 0
}

// --- retained bridge (deprecated; migrate callers to NewAnchoredDisclosureNode) ---

func buildAnchoredDisclosure[M any](ctx *retained.Context[M], parent retained.ViewSpec[M], details []retained.ViewSpec[M]) retained.RenderNode {
	children := make([]retained.ViewSpec[M], 0, len(details)+1)
	if ctx == nil {
		if parent != nil {
			children = append(children, parent)
		}
		for _, detail := range details {
			if detail != nil {
				children = append(children, retained.Padded[M](detail, 0, 0, 0, 2))
			}
		}
	} else {
		if parent != nil {
			children = append(children, retained.Named[M]("parent", parent))
		}
		for i, detail := range details {
			if detail != nil {
				children = append(children, retained.Named[M](fmt.Sprintf("detail/%d", i), retained.Padded[M](detail, 0, 0, 0, 2)))
			}
		}
	}
	return retained.Materialize(ctx, retained.VStack(ctx, children...))
}

// NewAnchoredDisclosure is the retained wrapper. Migrate callers to
// NewAnchoredDisclosureNode.
func NewAnchoredDisclosure[M any](parent retained.ViewSpec[M], details ...retained.ViewSpec[M]) retained.ViewSpec[M] {
	return retained.View[M](func(ctx *retained.Context[M]) retained.RenderNode {
		return buildAnchoredDisclosure(ctx, parent, details)
	})
}
