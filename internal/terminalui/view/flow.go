package view

type FlowAxis uint8

const (
	FlowAxisColumn FlowAxis = iota
	FlowAxisRow
)

type FlowSpec struct {
	Axis FlowAxis
	Gap  int
}

