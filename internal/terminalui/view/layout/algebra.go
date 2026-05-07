package layout

// MeasurePlaceNode is the minimal strategy-neutral layout contract.
// Implementations may render retained scenes or append-only transcripts.
type MeasurePlaceNode interface {
	Measure(Constraints) Size
	Place(Rect) []PlacedChild
}

type PlacedChild struct {
	Rect Rect
	Node MeasurePlaceNode
}
