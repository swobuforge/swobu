package core

// Axis is the semantic axis used by stack-like layout composition.
type Axis uint8

const (
	AxisVertical Axis = iota
	AxisHorizontal
)

// DimMode selects how one dimension should resolve.
type DimMode uint8

const (
	DimFit DimMode = iota
	DimFixed
	DimFill
	DimMinMax
)

// Dim is one dimension of semantic layout.
type Dim struct {
	Mode   DimMode
	Value  int
	Min    int
	Max    int
	Weight int
}

// Fit resolves to content size.
func Fit() Dim { return Dim{Mode: DimFit} }

// Fixed resolves to one exact size.
func Fixed(n int) Dim { return Dim{Mode: DimFixed, Value: n} }

// Fill resolves to available space, weighted relative to siblings.
func Fill(weight int) Dim {
	if weight <= 0 {
		weight = 1
	}
	return Dim{Mode: DimFill, Weight: weight}
}

// MinMax resolves within a bounded range.
func MinMax(min int, max Dim) Dim {
	return Dim{Mode: DimMinMax, Min: min, Max: max.Value, Weight: max.Weight}
}

// Size carries semantic width and height dimensions.
type Size struct {
	Width  Dim
	Height Dim
}

// FlowMode selects the structural flow behavior.
type FlowMode uint8

const (
	FlowNone FlowMode = iota
	FlowStack
	FlowLayer
)

// Flow describes structural child composition.
type Flow struct {
	Mode FlowMode
	Axis Axis
	Gap  int
}

// AlignMode controls main-axis and cross-axis alignment.
type AlignMode uint8

const (
	AlignStart AlignMode = iota
	AlignCenter
	AlignEnd
	AlignStretch
)

// Align describes layout alignment intent.
type Align struct {
	Main  AlignMode
	Cross AlignMode
}

// Inset describes semantic padding/inset edges.
type Inset struct {
	Top, Right, Bottom, Left int
}

// Overflow controls overflow behavior.
type Overflow uint8

const (
	OverflowClip Overflow = iota
	OverflowScroll
	OverflowVisible
)

// Layout is the full semantic layout envelope.
type Layout struct {
	Size     Size
	Flow     Flow
	Align    Align
	Inset    Inset
	Overflow Overflow
}
