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

// DimSize is one dimension of semantic layout.
type DimSize struct {
	Mode       DimMode
	Value      int
	Min        int
	Max        int
	Weight     int
	UBoundMode DimMode // upper-bound mode preserved from MinMax(max)
}

// Fit resolves to content size.
func Fit() DimSize { return DimSize{Mode: DimFit} }

// Fixed resolves to one exact size.
func Fixed(n int) DimSize { return DimSize{Mode: DimFixed, Value: n} }

// Fill resolves to available space, weighted relative to siblings.
func Fill(weight int) DimSize {
	if weight <= 0 {
		weight = 1
	}
	return DimSize{Mode: DimFill, Weight: weight}
}

// MinMax resolves within a bounded range.
//
// The returned DimSize preserves the max mode as part of the upper-bound
// contract so the layout pass can distinguish Fixed, Fill, and Weighted
// ceilings.  The upper-bound DimSize must not itself be DimFit; use
// Fit() for unbounded sizing.
func MinMax(min int, max DimSize) DimSize {
	if max.Mode == DimFit {
		panic("core.MinMax: max mode cannot be DimFit; use Fit() for unbounded sizing")
	}
	return DimSize{Mode: DimMinMax, Min: min, Max: max.Value, Weight: max.Weight, UBoundMode: max.Mode}
}

// Size carries semantic width and height dimensions.
type Size struct {
	Width  DimSize
	Height DimSize
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

// AlignPolicy describes layout alignment intent.
type AlignPolicy struct {
	Main  AlignMode
	Cross AlignMode
}

// Insets describes semantic padding/inset edges.
type Insets struct {
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
	Align    AlignPolicy
	Inset    Insets
	Overflow Overflow
}
