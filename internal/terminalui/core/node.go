package core

// Kind identifies the semantic node family.
type Kind uint8

const (
	KindText Kind = iota
	KindBox
	KindStack
	KindLayer
	KindScroll
	KindAction
	KindInput
	KindList
	KindTable
)

// Content carries one node's semantic payload.
type Content struct {
	Text string
}

// Debug carries optional authoring metadata.
type Debug struct {
	Name string
	File string
}

// Node is one immutable semantic UI node.
type Node struct {
	key          Key
	kind         Kind
	stateful     bool
	layout       Layout
	style        Style
	interaction  Interaction
	contract     Contract
	content      Content
	children     []Node
	scrollOffset int
	debug        Debug
}

// Text creates one semantic text node.
func Text(value string) Node {
	return Node{
		kind:    KindText,
		content: Content{Text: value},
		layout:  Layout{Size: Size{Width: Fit(), Height: Fit()}},
		style:   Style{Token: TokenTextDefault},
	}
}

// Box creates one semantic container node.
func Box(children ...Node) Node {
	return Node{
		kind:     KindBox,
		children: cloneNodes(children),
		layout:   Layout{Size: Size{Width: Fill(1), Height: Fit()}},
		style:    Style{Token: TokenSurfaceDefault},
	}
}

// Stack creates one semantic stack node.
func Stack(axis Axis, children ...Node) Node {
	return Node{
		kind:     KindStack,
		children: cloneNodes(children),
		layout: Layout{
			Size: Size{Width: Fill(1), Height: Fit()},
			Flow: Flow{Mode: FlowStack, Axis: axis},
		},
		style: Style{Token: TokenSurfaceDefault},
	}
}

// Layer creates one semantic overlay node.
func Layer(children ...Node) Node {
	return Node{
		kind:     KindLayer,
		children: cloneNodes(children),
		layout: Layout{
			Size: Size{Width: Fill(1), Height: Fill(1)},
			Flow: Flow{Mode: FlowLayer},
		},
		style: Style{Token: TokenSurfaceDefault},
	}
}

// Scroll creates one vertical scroll container.
func Scroll(child Node, offset int) Node {
	return Node{
		kind:     KindScroll,
		children: []Node{child},
		layout: Layout{
			Size:     Size{Width: Fill(1), Height: Fill(1)},
			Overflow: OverflowScroll,
		},
		scrollOffset: offset,
		style:        Style{Token: TokenSurfaceDefault},
		debug:        Debug{Name: "scroll", File: ""},
	}
}

// Input creates one focusable semantic text input.
func Input(value string) Node {
	return Node{
		kind:    KindInput,
		content: Content{Text: value},
		layout:  Layout{Size: Size{Width: Fill(1), Height: Fit()}},
		style:   Style{Token: TokenTextDefault},
		interaction: Interaction{
			Focus: FocusSpec{Mode: Focusable},
		},
	}
}

// Stateful marks one node as retaining local semantic state or participating in
// a dynamic identity-sensitive collection.
func (n Node) Stateful() Node {
	n.stateful = true
	return n
}

// Action creates one focusable action node that emits one signal when activated.
func Action(label string, signal Signal) Node {
	node := Text(label)
	node.kind = KindAction
	node.interaction = Interaction{
		Focus: FocusSpec{Mode: Focusable},
		Keymap: []KeyBinding{{
			Pattern: KeyEnter(),
			Intent:  IntentActivate,
		}},
		Help: []HelpBinding{
			{Key: "enter", Label: "activate"},
		},
		Signals: []Signal{signal},
	}
	node.contract = Contract{
		Name:    "Action",
		Purpose: "Focusable semantic action.",
		Signals: []SignalSpec{{Kind: signal.Kind}},
		Layout: LayoutGuarantee{
			Width:  Fill(1),
			Height: Fit(),
		},
		Focus: FocusGuarantee{FocusableWhenEnabled: true},
		Help: []HelpBinding{
			{Key: "enter", Label: "activate"},
		},
	}
	return node
}

// Key returns a copy of the node with a stable identity key.
func (n Node) Key(key Key) Node {
	n.key = key
	return n
}

// Layout returns a copy of the node with the supplied layout.
func (n Node) Layout(layout Layout) Node {
	n.layout = layout
	return n
}

// Style returns a copy of the node with the supplied style.
func (n Node) Style(style Style) Node {
	n.style = style
	return n
}

// Interaction returns a copy of the node with the supplied interaction contract.
func (n Node) Interaction(interaction Interaction) Node {
	n.interaction = cloneInteraction(interaction)
	return n
}

// Contract returns a copy of the node with the supplied contract.
func (n Node) Contract(contract Contract) Node {
	n.contract = cloneContract(contract)
	return n
}

// Children returns a copy of the node with a new child slice.
func (n Node) Children(children ...Node) Node {
	n.children = cloneNodes(children)
	return n
}

// Debug returns a copy of the node with the supplied debug metadata.
func (n Node) Debug(debug Debug) Node {
	n.debug = debug
	return n
}

// Kind returns the node kind.
func (n Node) Kind() Kind { return n.kind }

// KeyValue returns the node key.
func (n Node) KeyValue() Key { return n.key }

// LayoutValue returns the node layout.
func (n Node) LayoutValue() Layout { return n.layout }

// StyleValue returns the node style.
func (n Node) StyleValue() Style { return n.style }

// InteractionValue returns the node interaction contract.
func (n Node) InteractionValue() Interaction { return cloneInteraction(n.interaction) }

// ContractValue returns the node contract.
func (n Node) ContractValue() Contract { return cloneContract(n.contract) }

// ContentValue returns the node content.
func (n Node) ContentValue() Content { return n.content }

// ChildrenValue returns a copy of the node's children.
func (n Node) ChildrenValue() []Node { return cloneNodes(n.children) }

// DebugValue returns the node debug metadata.
func (n Node) DebugValue() Debug { return n.debug }

// ScrollOffsetValue returns the semantic scroll offset.
func (n Node) ScrollOffsetValue() int { return n.scrollOffset }

func cloneNodes(children []Node) []Node {
	return append([]Node(nil), children...)
}

func cloneInteraction(interaction Interaction) Interaction {
	interaction.Keymap = append([]KeyBinding(nil), interaction.Keymap...)
	interaction.Help = append([]HelpBinding(nil), interaction.Help...)
	interaction.Signals = append([]Signal(nil), interaction.Signals...)
	interaction.FocusSignals = append([]Signal(nil), interaction.FocusSignals...)
	return interaction
}

func cloneContract(contract Contract) Contract {
	contract.Props = append([]PropSpec(nil), contract.Props...)
	contract.Signals = append([]SignalSpec(nil), contract.Signals...)
	contract.Requires = append([]Capability(nil), contract.Requires...)
	contract.Slots = append([]SlotSpec(nil), contract.Slots...)
	contract.Help = append([]HelpBinding(nil), contract.Help...)
	contract.States = append([]VisualState(nil), contract.States...)
	return contract
}
