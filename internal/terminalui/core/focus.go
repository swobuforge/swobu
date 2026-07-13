package core

// FocusMode describes how a node participates in focus.
type FocusMode uint8

const (
	FocusNone FocusMode = iota
	Focusable
	FocusGroup
	FocusScope
)

// FocusID is the stable identity token for a focusable node in the focus graph.
type FocusID string

// Empty reports whether the FocusID has no meaningful content.
func (id FocusID) Empty() bool {
	return string(id) == ""
}

// FocusSpec is one node's focus contract.
type FocusSpec struct {
	Mode FocusMode
	// ID is the stable focus identity used by the runtime focus graph.
	// When empty, the runtime assigns an implicit identity from layout position.
	ID   FocusID
	Trap bool
}
