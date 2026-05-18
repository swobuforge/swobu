package layout

// Cell is the canonical spatial unit for terminal layout algebra.
type Cell int

type Size struct {
	W Cell
	H Cell
}

type RectLayout struct {
	X Cell
	Y Cell
	W Cell
	H Cell
}

type Constraints struct {
	MinW Cell
	MaxW Cell
	MinH Cell
	MaxH Cell
}

type Axis uint8

const (
	AxisColumn Axis = iota
	AxisRow
)

type FlexProps struct {
	Axis Axis
	Gap  Cell
}

type Insets struct {
	Top    Cell
	Right  Cell
	Bottom Cell
	Left   Cell
}

type ScrollAxis uint8

const (
	ScrollAxisY ScrollAxis = iota
)

type ScrollProps struct {
	Axis   ScrollAxis
	Offset Cell
}

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

type Point struct {
	X Cell
	Y Cell
}

type Placement struct {
	Ref    PlacementRef
	Anchor Anchor
	Offset Point
}

type StackChild[T any] struct {
	Child     T
	Placement Placement
	Z         Cell
}
