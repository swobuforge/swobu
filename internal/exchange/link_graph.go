package exchange

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/transform"
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

// Middleware wraps one step without changing the step's input or output type.
type Middleware[I any, O any] func(next Step[I, O]) Step[I, O]

// Predicate decides whether one link or middleware is available in the current
// exchange context.
type Predicate func(Context) bool

// Tag labels one graph link for routing and diagnostics.
type Tag string

// Context contains exchange facts for graph evaluation.
type Context struct {
	Go         context.Context
	ExchangeID string
	Route      RouteSpec
	Target     *RoutableTarget
	Delivery   delivery.Delivery
	Attempt    *AttemptInfo
}

// AttemptInfo records one selected attempt while the builder is evaluating a
// port-local path.
type AttemptInfo struct {
	Index int
	ID    LinkID
}

// Result carries the next graph value plus any side effects the step wants
// committed outside the step itself.
type Result[T any] struct {
	Value   T
	Effects []effect.Effect
}

// NewResult clones the provided effects into one typed graph result.
func NewResult[T any](value T, effects ...effect.Effect) Result[T] {
	return Result[T]{
		Value:   value,
		Effects: append([]effect.Effect(nil), effects...),
	}
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

func documentPortForStage(stage transform.Stage) Port[carrier.WireDocument] {
	switch stage {
	case transform.StageClientWireIn:
		return NewPort[carrier.WireDocument](PortID("client.wire.in"))
	case transform.StageRequestDocumentOut:
		return NewPort[carrier.WireDocument](PortID("provider.request.wire_out"))
	case transform.StageRequestDocumentIn:
		return NewPort[carrier.WireDocument](PortID("provider.response.wire_in"))
	default:
		return NewPort[carrier.WireDocument](PortID(stage))
	}
}

func semanticEventsPort() Port[canonical.EventReader] {
	return NewPort[canonical.EventReader](PortID("semantic.response_events"))
}

func clientOutputPort() Port[canonical.CanonicalOutput] {
	return NewPort[canonical.CanonicalOutput](PortID("client.response.snapshot"))
}
