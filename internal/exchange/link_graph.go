package exchange

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/delivery"
)

// PortID names one typed location in the exchange graph.
type PortID string

// Port marks one typed location in the exchange graph.
type Port[T any] struct {
	id PortID
}

// NewPort constructs one typed exchange port.
func NewPort[T any](id PortID) Port[T] {
	return Port[T]{id: id}
}

// ID returns the stable port identifier.
func (p Port[T]) ID() PortID {
	return p.id
}

// IsZero reports whether the port is unnamed.
func (p Port[T]) IsZero() bool {
	return p.id == ""
}

// LinkID names one directed movement between ports.
type LinkID string

// Step is one typed exchange operation.
type Step[I any, O any] func(context.Context, I) (Result[O], error)

// Wrapper wraps one step without changing the step's input or output type.
type Wrapper[I any, O any] func(next Step[I, O]) Step[I, O]

// Predicate decides whether one link or stage wrapper is available in the current
// exchange context.
type Predicate func(Context) bool

// Tag labels one graph link for routing and diagnostics.
type Tag string

// Context contains exchange facts for graph evaluation.
type Context struct {
	Go         context.Context
	ExchangeID string
	Target     *RoutableTarget
	Delivery   delivery.Delivery
}

// Link connects one typed port to another with one step.
type Link[I any, O any] struct {
	ID     LinkID
	From   Port[I]
	To     Port[O]
	Step   Step[I, O]
	When   []Predicate
	After  []LinkID
	Before []LinkID
	Cost   int
	Tags   []Tag
}

// NewLink constructs one typed graph link.
func NewLink[I any, O any](id LinkID, from Port[I], to Port[O], step Step[I, O], opts ...LinkOption[I, O]) Link[I, O] {
	link := Link[I, O]{
		ID:   id,
		From: from,
		To:   to,
		Step: step,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&link)
		}
	}
	return link
}

// Run executes the step attached to the link.
func (l Link[I, O]) Run(ctx context.Context, input I) (Result[O], error) {
	if l.Step == nil {
		return Result[O]{}, errors.New("exchange link step is required")
	}
	return l.Step(ctx, input)
}
