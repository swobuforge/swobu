package layout

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

type NodeID uint64

type LayoutContext struct{}
type PaintContext struct {
	FocusedID NodeID
}

// RenderNode is the effect-free structural contract for retained layout and paint.
type RenderNode interface {
	Measure(c geom.Constraints, ctx *LayoutContext) geom.Size
	Arrange(node *LayoutNode, ctx *LayoutContext) NodeLayout
	Paint(p paint.Painter, node *LayoutNode, ctx *PaintContext)
}

// Composite is an optional structural interface for nodes that own direct
// child nodes and can rebuild themselves with those children rewritten.
//
// Reconciliation uses this seam to recurse through the retained UI tree without
// depending on the concrete layout operator set.
//
// VisitChildren and MapChildren must expose direct children in the same stable
// order for one concrete node type.
type Composite interface {
	RenderNode
	VisitChildren(visit func(hint string, child RenderNode))
	MapChildren(rewrite func(hint string, child RenderNode) RenderNode) RenderNode
}

// LayoutNode is the retained computed artifact for one resolved node.
type LayoutNode struct {
	ID           NodeID
	RenderNode   RenderNode
	Parent       *LayoutNode
	Kids         []*LayoutNode
	Slot         geom.Rect
	BorderRect   geom.Rect
	ContentRect  geom.Rect
	ViewportRect geom.Rect
	ClipRect     geom.Rect
	ContentSize  geom.Size
	ScrollOffset geom.Point
	MeasuredSize geom.Size
	Z            int
}

// NodeLayout is one node's resolved local geometry and direct child slots.
// The runtime materializes this local result into the retained LayoutNode tree.
type NodeLayout struct {
	BorderRect   geom.Rect
	ContentRect  geom.Rect
	ViewportRect geom.Rect
	ContentSize  geom.Size
	ScrollOffset geom.Point
	ChildSlots   []ChildSlot
}

type ChildSlot struct {
	Spec ChildSpec
	Rect geom.Rect
}

type Participation uint8

const (
	InFlow Participation = iota
	OutOfFlow
)

type PlacementRef uint8

const (
	RefSlot PlacementRef = iota
)

type Anchor uint8

const (
	AnchorTopLeft Anchor = iota
	AnchorTop
	AnchorTopRight
	AnchorLeft
	AnchorCenter
	AnchorRight
	AnchorBottomLeft
	AnchorBottom
	AnchorBottomRight
)

type Placement struct {
	Ref    PlacementRef
	Anchor Anchor
	Offset geom.Point
}

type ChildSpec struct {
	Hint          string
	RenderNode    RenderNode
	Participation Participation
	Placement     Placement
	Z             int
}

type identityRenderNode struct {
	RenderNode
	id NodeID
}

func (e identityRenderNode) nodeID() NodeID     { return e.id }
func (e identityRenderNode) unwrap() RenderNode { return e.RenderNode }

// WithIdentity annotates one structural node with a stable retained node ID
// supplied by the reconciler.
func WithIdentity(id NodeID, node RenderNode) RenderNode {
	return identityRenderNode{RenderNode: node, id: id}
}

// IdentityOf reports the retained node identity attached to one structural
// node, if present.
func IdentityOf(node RenderNode) (NodeID, bool) {
	identified, ok := node.(interface{ nodeID() NodeID })
	if !ok {
		return 0, false
	}
	return identified.nodeID(), true
}

// UnwrapIdentity returns the inner node wrapped by WithIdentity, or the
// node itself if no identity wrapper is present.
func UnwrapIdentity(node RenderNode) RenderNode {
	if ie, ok := node.(identityRenderNode); ok {
		return ie.RenderNode
	}
	return node
}

func PlaceWithin(ref geom.Rect, self geom.Size, anchor Anchor, offset geom.Point) geom.Rect {
	var x, y int
	switch anchor {
	case AnchorTopLeft:
		x, y = ref.X, ref.Y
	case AnchorTop:
		x, y = ref.X+(ref.W-self.W)/2, ref.Y
	case AnchorTopRight:
		x, y = ref.Right()-self.W, ref.Y
	case AnchorLeft:
		x, y = ref.X, ref.Y+(ref.H-self.H)/2
	case AnchorCenter:
		x, y = ref.X+(ref.W-self.W)/2, ref.Y+(ref.H-self.H)/2
	case AnchorRight:
		x, y = ref.Right()-self.W, ref.Y+(ref.H-self.H)/2
	case AnchorBottomLeft:
		x, y = ref.X, ref.Bottom()-self.H
	case AnchorBottom:
		x, y = ref.X+(ref.W-self.W)/2, ref.Bottom()-self.H
	case AnchorBottomRight:
		x, y = ref.Right()-self.W, ref.Bottom()-self.H
	}
	return geom.Rect{X: x + offset.X, Y: y + offset.Y, W: self.W, H: self.H}
}

type SizeMode uint8

const (
	SizeFit SizeMode = iota
	SizeGrow
	SizeFixed
)

// Sizing is the minimal per-axis sizing policy for author-facing combinators.
type Sizing struct {
	W     SizeMode
	H     SizeMode
	Fixed geom.Size
	Min   geom.Size
	Max   geom.Size
}

// combinator over independent axis modes and constraint clamps.
func (s Sizing) ResolveIntrinsic(intrinsic geom.Size, c geom.Constraints) geom.Size {
	out := intrinsic
	switch s.W {
	case SizeFit:
		// keep intrinsic width
	case SizeFixed:
		out.W = s.Fixed.W
	case SizeGrow:
		if c.W.Max >= 0 {
			out.W = c.W.Max
		}
	}
	switch s.H {
	case SizeFit:
		// keep intrinsic height
	case SizeFixed:
		out.H = s.Fixed.H
	case SizeGrow:
		if c.H.Max >= 0 {
			out.H = c.H.Max
		}
	}
	if out.W < s.Min.W {
		out.W = s.Min.W
	}
	if out.H < s.Min.H {
		out.H = s.Min.H
	}
	if s.Max.W > 0 && out.W > s.Max.W {
		out.W = s.Max.W
	}
	if s.Max.H > 0 && out.H > s.Max.H {
		out.H = s.Max.H
	}
	return geom.ClampSize(out, c)
}

// Sized is one embeddable helper for node-local sizing policy.
type Sized struct {
	Sizing Sizing
}

func (s Sized) ResolveSize(intrinsic geom.Size, c geom.Constraints) geom.Size {
	if s.Sizing == (Sizing{}) {
		return geom.ClampSize(intrinsic, c)
	}
	return s.Sizing.ResolveIntrinsic(intrinsic, c)
}
