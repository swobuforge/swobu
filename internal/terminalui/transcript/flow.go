package transcript

// FlowAxis selects the orientation used by FlowSpec.
type FlowAxis uint8

const (
	FlowAxisColumn FlowAxis = iota
	FlowAxisRow
)

// FlowSpec arranges children in a row or column with a fixed gap.
type FlowSpec struct {
	Axis FlowAxis
	Gap  int
}
