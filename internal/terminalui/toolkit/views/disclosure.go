// Disclosure views compose parent and detail views into anchored sections.
package views

import (
	"fmt"

	"github.com/metrofun/swobu/internal/terminalui/engine/retained/view"
)

func buildAnchoredDisclosure[M any](ctx *view.Context[M], parent view.ViewSpec[M], details []view.ViewSpec[M]) view.RenderNode {
	children := make([]view.ViewSpec[M], 0, len(details)+1)
	if ctx == nil {
		if parent != nil {
			children = append(children, parent)
		}
		for _, detail := range details {
			if detail != nil {
				children = append(children, view.WithPadLeft[M](2)(detail))
			}
		}
	} else {
		if parent != nil {
			children = append(children, view.Named[M]("parent", parent))
		}
		for i, detail := range details {
			if detail != nil {
				children = append(children, view.Named[M](fmt.Sprintf("detail/%d", i), view.WithPadLeft[M](2)(detail)))
			}
		}
	}
	return view.Materialize(ctx, view.VStack(ctx, children...))
}

// NewAnchoredDisclosure is the constructor form for convenience.
func NewAnchoredDisclosure[M any](parent view.ViewSpec[M], details ...view.ViewSpec[M]) view.ViewSpec[M] {
	return view.View[M](func(ctx *view.Context[M]) view.RenderNode {
		return buildAnchoredDisclosure(ctx, parent, details)
	})
}
