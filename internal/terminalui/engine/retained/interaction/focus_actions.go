package interaction

type FocusMove uint8

const (
	FocusMoveNext FocusMove = iota
	FocusMovePrev
)

// FocusMoveAction requests generic focus traversal in the retained runtime.
// This is engine-level interaction semantics, not app product intent.
type FocusMoveAction struct {
	Move FocusMove
}

// FocusKeyAction requests focusing the first focusable node with a matching
// named-view identity. Runtime may defer resolution until after rebuild.
type FocusKeyAction struct {
	Key string
}
