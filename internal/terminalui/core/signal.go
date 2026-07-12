package core

// Signal is a generic semantic event emitted by interactive nodes.
type Signal struct {
	Kind string
	Data any
}
