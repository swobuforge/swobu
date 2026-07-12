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

// FocusPolicy describes how focus should behave for the node.
type FocusPolicy struct {
	FocusableWhenEnabled bool
	TrapsFocus           bool
}

// LayoutPolicy describes the node's minimum layout promise.
type LayoutPolicy struct {
	Height DimSize
	Width  DimSize
}

// Contract is the runtime-readable semantic contract for one node.
type Contract struct {
	Name     string
	Purpose  string
	Props    []PropSpec
	Signals  []SignalSpec
	Requires []Capability
	Slots    []SlotSpec
	Layout   LayoutPolicy
	Focus    FocusPolicy
	Help     []HelpBindingSpec
	States   []VisualState
}
