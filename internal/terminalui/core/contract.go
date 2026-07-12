package core

// PropSpec describes one contract property.
type PropSpec struct {
	Name     string
	Required bool
	Summary  string
}

// SignalSpec describes one contract-emitted signal kind.
type SignalSpec struct {
	Kind string
}

// SlotSpec describes one named child slot.
type SlotSpec struct {
	Name     string
	Required bool
	Summary  string
}

// FocusGuarantee describes how focus should behave for the node.
type FocusGuarantee struct {
	FocusableWhenEnabled bool
	TrapsFocus           bool
}

// LayoutGuarantee describes the node's minimum layout promise.
type LayoutGuarantee struct {
	Height Dim
	Width  Dim
}

// Contract is the runtime-readable semantic contract for one node.
type Contract struct {
	Name     string
	Purpose  string
	Props    []PropSpec
	Signals  []SignalSpec
	Requires []Capability
	Slots    []SlotSpec
	Layout   LayoutGuarantee
	Focus    FocusGuarantee
	Help     []HelpBinding
	States   []VisualState
}
