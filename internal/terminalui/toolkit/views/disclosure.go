// Disclosure views compose parent and detail views into anchored sections.
package views

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func buildAnchoredDisclosure[M any](ctx *retained.Context[M], parent retained.ViewSpec[M], details []retained.ViewSpec[M]) retained.RenderNode {
	children := make([]retained.ViewSpec[M], 0, len(details)+1)
	if ctx == nil {
		if parent != nil {
			children = append(children, parent)
		}
		for _, detail := range details {
			if detail != nil {
				children = append(children, retained.WithPadLeft[M](2)(detail))
			}
		}
	} else {
		if parent != nil {
			children = append(children, retained.Named[M]("parent", parent))
		}
		for i, detail := range details {
			if detail != nil {
				children = append(children, retained.Named[M](fmt.Sprintf("detail/%d", i), retained.WithPadLeft[M](2)(detail)))
			}
		}
	}
	return retained.Materialize(ctx, retained.VStack(ctx, children...))
}

// NewAnchoredDisclosure is the constructor form for convenience.
func NewAnchoredDisclosure[M any](parent retained.ViewSpec[M], details ...retained.ViewSpec[M]) retained.ViewSpec[M] {
	return retained.View[M](func(ctx *retained.Context[M]) retained.RenderNode {
		return buildAnchoredDisclosure(ctx, parent, details)
	})
}
