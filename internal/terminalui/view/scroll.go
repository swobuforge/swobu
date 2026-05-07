package view

type ScrollAxis uint8

const (
	ScrollAxisY ScrollAxis = iota
)

type ScrollSpec struct {
	Axis   ScrollAxis
	Offset int
}
