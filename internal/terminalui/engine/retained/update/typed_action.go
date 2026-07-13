package update

// TypedAction[E] carries one semantic event emitted by a lowered core node.
// It is the bridge from the retained runtime back to typed app events.
type TypedAction[E any] struct {
	Event E
}
