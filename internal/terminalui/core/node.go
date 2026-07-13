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

// ContentPayload carries one node's semantic payload.
type ContentPayload struct {
	Text string
}

// DebugMetadata carries optional authoring metadata.
type DebugMetadata struct {
	Name string
	File string
}

// Node[E] is one immutable semantic UI node parameterized by event type E.
type Node[E any] struct {
	key          Key
	kind         Kind
	stateful     bool
	layout       Layout
	style        Style
	interaction  InteractionSpec[E]
	contract     Contract[E]
	content      ContentPayload
	children     []Node[E]
	scrollOffset int
	debug        DebugMetadata
}

// Text creates one semantic text node.
func Text[E any](value string) Node[E] {
	return Node[E]{
		kind:    KindText,
		content: ContentPayload{Text: value},
		layout:  Layout{Size: Size{Width: Fit(), Height: Fit()}},
		style:   Style{Token: TokenTextDefault},
	}
}

// Box creates one semantic container node.
func Box[E any](children ...Node[E]) Node[E] {
	return Node[E]{
		kind:     KindBox,
		children: cloneNodes(children),
		layout:   Layout{Size: Size{Width: Fill(1), Height: Fit()}},
		style:    Style{Token: TokenSurfaceDefault},
	}
}

// Stack creates one semantic stack node.
func Stack[E any](axis Axis, children ...Node[E]) Node[E] {
	return Node[E]{
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
func Layer[E any](children ...Node[E]) Node[E] {
	return Node[E]{
		kind:     KindLayer,
		children: cloneNodes(children),
		layout: Layout{
			Size: Size{Width: Fill(1), Height: Fill(1)},
			Flow: Flow{Mode: FlowLayer},
		},
		style: Style{Token: TokenSurfaceDefault},
	}
}

// Scroll creates one semantic scroll viewport wrapping a child.
func Scroll[E any](offset int, children ...Node[E]) Node[E] {
	return Node[E]{
		kind:         KindScroll,
		children:     cloneNodes(children),
		scrollOffset: offset,
		layout: Layout{
			Size: Size{Width: Fill(1), Height: Fill(1)},
		},
		style: Style{Token: TokenSurfaceDefault},
	}
}

// Input creates one focusable semantic text input.
func Input[E any](value string) Node[E] {
	return Node[E]{
		kind:    KindInput,
		content: ContentPayload{Text: value},
		layout:  Layout{Size: Size{Width: Fill(1), Height: Fit()}},
		style:   Style{Token: TokenTextDefault},
		interaction: InteractionSpec[E]{
			Focus: FocusSpec{Mode: Focusable},
		},
	}
}

// Stateful marks one node as retaining local semantic state or participating in
// a dynamic identity-sensitive collection.
func (n Node[E]) Stateful() Node[E] {
	n.stateful = true
	return n
}

// Action creates one focusable action node that emits one signal when activated.
func Action[E any](label string, signal SignalEvent[E]) Node[E] {
	node := Text[E](label)
	node.kind = KindAction
	node.interaction = InteractionSpec[E]{
		Focus: FocusSpec{Mode: Focusable},
		Keymap: []KeyBindingSpec{{
			Pattern: KeyEnter(),
			Intent:  IntentActivate,
		}},
		Help: []HelpBindingSpec{
			{Key: "enter", Label: "activate"},
		},
		Signals: []SignalEvent[E]{signal},
	}
	node.contract = Contract[E]{
		Name:    "Action",
		Purpose: "Focusable semantic action.",
		Signals: []SignalSpec[E]{{Kind: signal.Kind}},
		Layout: LayoutPolicy{
			Width:  Fill(1),
			Height: Fit(),
		},
		Focus: FocusPolicy{FocusableWhenEnabled: true},
		Help: []HelpBindingSpec{
			{Key: "enter", Label: "activate"},
		},
	}
	return node
}

// Key returns a copy of the node with a stable identity key.
func (n Node[E]) Key(key Key) Node[E] {
	n.key = key
	return n
}

// Layout returns a copy of the node with the supplied layout.
func (n Node[E]) Layout(layout Layout) Node[E] {
	n.layout = layout
	return n
}

// Style returns a copy of the node with the supplied style.
func (n Node[E]) Style(style Style) Node[E] {
	n.style = style
	return n
}

// Interaction returns a copy of the node with the supplied interaction contract.
func (n Node[E]) Interaction(interaction InteractionSpec[E]) Node[E] {
	n.interaction = cloneInteraction(interaction)
	return n
}

// Contract returns a copy of the node with the supplied contract.
func (n Node[E]) Contract(contract Contract[E]) Node[E] {
	n.contract = cloneContract(contract)
	return n
}

// Children returns a copy of the node with a new child slice.
func (n Node[E]) Children(children ...Node[E]) Node[E] {
	n.children = cloneNodes(children)
	return n
}

// ScrollOffset returns a copy of the node with the supplied scroll offset.
func (n Node[E]) ScrollOffset(offset int) Node[E] {
	n.scrollOffset = offset
	return n
}

// Debug returns a copy of the node with the supplied debug metadata.
func (n Node[E]) Debug(debug DebugMetadata) Node[E] {
	n.debug = debug
	return n
}

// Kind returns the node kind.
func (n Node[E]) Kind() Kind { return n.kind }

// KeyValue returns the node key.
func (n Node[E]) KeyValue() Key { return n.key }

// LayoutValue returns the node layout.
func (n Node[E]) LayoutValue() Layout { return n.layout }

// StyleValue returns the node style.
func (n Node[E]) StyleValue() Style { return n.style }

// InteractionValue returns the node interaction contract.
func (n Node[E]) InteractionValue() InteractionSpec[E] { return cloneInteraction(n.interaction) }

// ContractValue returns the node contract.
func (n Node[E]) ContractValue() Contract[E] { return cloneContract(n.contract) }

// ContentValue returns the node content.
func (n Node[E]) ContentValue() ContentPayload { return n.content }

// ChildrenValue returns a copy of the node's children.
func (n Node[E]) ChildrenValue() []Node[E] { return cloneNodes(n.children) }

// DebugValue returns the node debug metadata.
func (n Node[E]) DebugValue() DebugMetadata { return n.debug }

// ScrollOffsetValue returns the semantic scroll offset.
func (n Node[E]) ScrollOffsetValue() int { return n.scrollOffset }

func cloneNodes[E any](children []Node[E]) []Node[E] {
	return append([]Node[E](nil), children...)
}

func cloneInteraction[E any](interaction InteractionSpec[E]) InteractionSpec[E] {
	interaction.Keymap = append([]KeyBindingSpec(nil), interaction.Keymap...)
	interaction.Help = append([]HelpBindingSpec(nil), interaction.Help...)
	interaction.Signals = append([]SignalEvent[E](nil), interaction.Signals...)
	interaction.FocusSignals = append([]SignalEvent[E](nil), interaction.FocusSignals...)
	return interaction
}

func cloneContract[E any](contract Contract[E]) Contract[E] {
	contract.Props = append([]PropSpec(nil), contract.Props...)
	contract.Signals = append([]SignalSpec[E](nil), contract.Signals...)
	contract.Requires = append([]Capability(nil), contract.Requires...)
	contract.Slots = append([]SlotSpec(nil), contract.Slots...)
	contract.Help = append([]HelpBindingSpec(nil), contract.Help...)
	contract.States = append([]VisualState(nil), contract.States...)
	return contract
}
