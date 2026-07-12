package transcript

// ScrollAxis selects the scroll direction for ScrollSpec.
type ScrollAxis uint8

const (
	ScrollAxisY ScrollAxis = iota
)

// ScrollSpec projects a child as a clipped scrollable transcript region.
type ScrollSpec struct {
	Axis   ScrollAxis
	Offset int
}
