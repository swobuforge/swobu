package core

// SignalEvent[E] carries one typed semantic event emitted by interactive nodes.
type SignalEvent[E any] struct {
	Kind  string
	Event E
}
