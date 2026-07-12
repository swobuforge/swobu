package core

// SignalEvent is a generic semantic event emitted by interactive nodes.
type SignalEvent struct {
	Kind string
	Data any
}
