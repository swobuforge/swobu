package interaction

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

type EventKind uint8

const (
	EventMouseDown EventKind = iota
	EventMouseUp
	EventMouseMove
	EventWheelUp
	EventWheelDown
	EventKey
)

type Event struct {
	Kind EventKind
	Pos  geom.Point
	Key  Key
	Rune rune
	Mods Modifiers
}

type Hittable interface {
	HitTest(local geom.Point, node *layout.LayoutNode) bool
}

type EventHandler interface {
	HandleEvent(ev Event, node *layout.LayoutNode) []update.Action
}

// EventTransformer participates in runtime bubbling with explicit event
// transformation. Returning nil consumes the event locally; returning the same
// or a modified event bubbles that value to the parent after local handling.
type EventTransformer interface {
	HandleEventTransform(ev Event, node *layout.LayoutNode) (*Event, []update.Action)
}

// ScopedEventHandler participates in runtime bubbling. Implementations may
// return handled=true with zero actions to consume an event locally.
type ScopedEventHandler interface {
	HandleScopedEvent(ev Event, node *layout.LayoutNode) (handled bool, actions []update.Action)
}

type Focusable interface {
	CanFocus(node *layout.LayoutNode) bool
}

type FocusEvents interface {
	OnFocus(node *layout.LayoutNode) []update.Action
	OnBlur(node *layout.LayoutNode) []update.Action
}

type Lifecycle interface {
	OnMount(node *layout.LayoutNode) []update.Action
	OnUnmount(node *layout.LayoutNode) []update.Action
}
