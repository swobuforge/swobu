package layout

import (
	"testing"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/geom"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/rendergraph/paint"
)

func TestBox_ComputesContentRect(t *testing.T) {
	root := NewBox(NewText("x"))
	root.Border = &SingleBorder
	root.Padding = geom.Insets{Top: 1, Right: 2, Bottom: 1, Left: 2}
	node := (&TreeBuilder{}).Build(root, geom.Rect{W: 20, H: 10})
	if got := node.ContentRect; got != (geom.Rect{X: 3, Y: 2, W: 14, H: 6}) {
		t.Fatalf("content rect = %#v", got)
	}
}

func TestColumn_PlacesChildrenTopToBottomWithGap(t *testing.T) {
	col := NewColumn(
		FlowChild{RenderNode: NewText("a")},
		FlowChild{RenderNode: NewText("b")},
		FlowChild{RenderNode: NewText("c")},
	)
	col.Gap = 1
	root := (&TreeBuilder{}).Build(col, geom.Rect{W: 10, H: 10})
	if len(root.Kids) != 3 {
		t.Fatalf("kids = %d, want 3", len(root.Kids))
	}
	if got := root.Kids[0].Slot; got != (geom.Rect{X: 0, Y: 0, W: 10, H: 1}) {
		t.Fatalf("kid0 slot = %#v", got)
	}
	if got := root.Kids[1].Slot; got != (geom.Rect{X: 0, Y: 2, W: 10, H: 1}) {
		t.Fatalf("kid1 slot = %#v", got)
	}
	if got := root.Kids[2].Slot; got != (geom.Rect{X: 0, Y: 4, W: 10, H: 1}) {
		t.Fatalf("kid2 slot = %#v", got)
	}
}

func TestOverlay_AnchorDoesNotAffectBaseLayout(t *testing.T) {
	base := NewBox(NewText("base"))
	base.Border = &SingleBorder
	overlay := NewOverlay(base, OverlayChild{
		RenderNode: NewText("x"),
		Placement: Placement{
			Ref: RefSlot, Anchor: AnchorBottomRight, Offset: geom.Point{X: -1, Y: -1},
		},
		Z: 10,
	})
	root := (&TreeBuilder{}).Build(overlay, geom.Rect{W: 20, H: 10})
	if len(root.Kids) != 2 {
		t.Fatalf("kids = %d, want 2", len(root.Kids))
	}
	if got := root.Kids[0].Slot; got != (geom.Rect{W: 20, H: 10}) {
		t.Fatalf("base slot = %#v", got)
	}
	if got := root.Kids[1].Slot; got != (geom.Rect{X: 18, Y: 8, W: 1, H: 1}) {
		t.Fatalf("overlay slot = %#v", got)
	}
}

func TestScrollY_PreservesViewportAndClampsOffset(t *testing.T) {
	s := NewScrollY(NewText("1\n2\n3\n4\n5"))
	s.Offset = 10
	root := (&TreeBuilder{}).Build(s, geom.Rect{W: 4, H: 2})
	if got := root.ViewportRect; got != (geom.Rect{W: 4, H: 2}) {
		t.Fatalf("viewport = %#v", got)
	}
	if got := root.ContentSize; got != (geom.Size{W: 4, H: 5}) {
		t.Fatalf("content size = %#v", got)
	}
	if got := root.ScrollOffset.Y; got != 3 {
		t.Fatalf("scroll offset = %d, want 3", got)
	}
	if got := root.Kids[0].Slot; got != (geom.Rect{X: 0, Y: -3, W: 4, H: 5}) {
		t.Fatalf("child slot = %#v", got)
	}
}

func TestPaint_RespectsClip(t *testing.T) {
	root := NewScrollY(NewText("1\n2\n3\n4"))
	root.Offset = 2
	tree := (&TreeBuilder{}).Build(root, geom.Rect{W: 4, H: 2})
	buf := paint.NewBuffer(geom.Rect{W: 4, H: 2})
	paintNode(tree, buf, &PaintContext{})
	if got := buf.String(); got != "3\n4" {
		t.Fatalf("buffer = %q, want 3/4", got)
	}
}

func TestComposite_MapChildren_RewritesWithoutReconcileKnowingConcreteTypes(t *testing.T) {
	root := NewOverlay(
		NewBox(NewText("base")),
		OverlayChild{RenderNode: NewText("extra")},
	)

	composite, ok := any(root).(Composite)
	if !ok {
		t.Fatalf("root type %T should implement Composite", root)
	}

	rewritten := composite.MapChildren(func(hint string, child RenderNode) RenderNode {
		return NewBox(NewText(hint))
	})

	overlay, ok := rewritten.(*OverlayRenderNode)
	if !ok {
		t.Fatalf("rewritten root type = %T, want *OverlayRenderNode", rewritten)
	}
	if _, ok := overlay.BaseChild.(*BoxRenderNode); !ok {
		t.Fatalf("base child type = %T, want *BoxRenderNode", overlay.BaseChild)
	}
	if _, ok := overlay.Extras[0].RenderNode.(*BoxRenderNode); !ok {
		t.Fatalf("extra child type = %T, want *BoxRenderNode", overlay.Extras[0].RenderNode)
	}
}

func paintNode(node *LayoutNode, p paint.Painter, ctx *PaintContext) {
	if node.ClipRect.Empty() || node.BorderRect.Empty() {
		return
	}
	scoped := p.WithClip(node.ClipRect).WithOrigin(geom.Point{X: node.BorderRect.X, Y: node.BorderRect.Y})
	node.RenderNode.Paint(scoped, node, ctx)
	for _, child := range node.Kids {
		paintNode(child, scoped, ctx)
	}
}
