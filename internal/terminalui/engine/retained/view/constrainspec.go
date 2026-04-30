package view

import (
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

type ConstrainSpec struct {
	MinW int
	MaxW int
	MinH int
	MaxH int

	GrowW bool
	GrowH bool

	FixedW int
	FixedH int
}

func Constrain[M any](child ViewSpec[M], spec ConstrainSpec) ViewSpec[M] {
	return constrainedView[M]{child: child, spec: spec}
}

type constrainedView[M any] struct {
	child ViewSpec[M]
	spec  ConstrainSpec
}

func (w constrainedView[M]) BuildRenderNode(ctx *Context[M]) RenderNode {
	el := w.child.BuildRenderNode(ctx)
	if el == nil {
		return nil
	}
	box := layout.NewBox(el)
	sizing := layout.Sizing{W: layout.SizeFit, H: layout.SizeFit}
	if w.spec.GrowW {
		sizing.W = layout.SizeGrow
	}
	if w.spec.GrowH {
		sizing.H = layout.SizeGrow
	}
	if w.spec.FixedW > 0 {
		sizing.W = layout.SizeFixed
		sizing.Fixed.W = w.spec.FixedW
	}
	if w.spec.FixedH > 0 {
		sizing.H = layout.SizeFixed
		sizing.Fixed.H = w.spec.FixedH
	}
	if w.spec.MinW > 0 {
		sizing.Min.W = w.spec.MinW
	}
	if w.spec.MinH > 0 {
		sizing.Min.H = w.spec.MinH
	}
	if w.spec.MaxW > 0 {
		sizing.Max.W = w.spec.MaxW
	}
	if w.spec.MaxH > 0 {
		sizing.Max.H = w.spec.MaxH
	}
	box.Sized = layout.Sized{Sizing: sizing}
	return box
}

type Insets struct {
	Top    int
	Right  int
	Bottom int
	Left   int
}

func Inset[M any](child ViewSpec[M], in Insets) ViewSpec[M] {
	return Padded(child, in.Top, in.Right, in.Bottom, in.Left)
}

func ScrollY[M any](child ViewSpec[M], offset int) ViewSpec[M] {
	return scrollYView[M]{child: child, offset: offset}
}

type scrollYView[M any] struct {
	child  ViewSpec[M]
	offset int
}

func (w scrollYView[M]) BuildRenderNode(ctx *Context[M]) RenderNode {
	el := w.child.BuildRenderNode(ctx)
	if el == nil {
		return nil
	}
	s := layout.NewScrollY(el)
	s.Offset = w.offset
	s.Sized = layout.Sized{Sizing: layout.Sizing{W: layout.SizeGrow, H: layout.SizeGrow}}
	return s
}

func AtMost(w, h int) ConstrainSpec {
	return ConstrainSpec{MaxW: w, MaxH: h}
}

func MinSize(w, h int) ConstrainSpec {
	return ConstrainSpec{MinW: w, MinH: h}
}

func FixedSize(w, h int) ConstrainSpec {
	return ConstrainSpec{FixedW: w, FixedH: h}
}

func FullSize() ConstrainSpec {
	return ConstrainSpec{GrowW: true, GrowH: true}
}
