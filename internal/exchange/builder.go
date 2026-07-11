package exchange

import (
	"context"
	"fmt"
	"sort"

	"github.com/swobuforge/swobu/internal/effect"
)

// LinkOption configures one same-port link before it is registered with a
// builder.
type LinkOption[I any, O any] func(*Link[I, O])

// When marks one link as available only when every predicate passes.
func When[I any, O any](predicates ...Predicate) LinkOption[I, O] {
	return func(link *Link[I, O]) {
		link.When = append(link.When, predicates...)
	}
}

// After declares that one link should run after the provided link IDs when the
// dependencies are present.
func After[I any, O any](ids ...LinkID) LinkOption[I, O] {
	return func(link *Link[I, O]) {
		link.After = append(link.After, ids...)
	}
}

// Before declares that one link should run before the provided link IDs when
// the dependencies are present.
func Before[I any, O any](ids ...LinkID) LinkOption[I, O] {
	return func(link *Link[I, O]) {
		link.Before = append(link.Before, ids...)
	}
}

// Cost records a link cost for future path selection. The same-port builder
// slice does not use it yet, but the field exists to keep link metadata aligned
// with the inbox design.
func Cost[I any, O any](cost int) LinkOption[I, O] {
	return func(link *Link[I, O]) {
		link.Cost = cost
	}
}

// Tags labels one link for diagnostics and future route selection.
func Tags[I any, O any](tags ...Tag) LinkOption[I, O] {
	return func(link *Link[I, O]) {
		link.Tags = append(link.Tags, tags...)
	}
}

type middlewareRegistration[T any] struct {
	middleware Middleware[T, T]
	when       []Predicate
}

// Builder assembles a same-port link chain and middleware wrappers for one
// exchange port visit.
type Builder[T any] struct {
	port        Port[T]
	links       []Link[T, T]
	middlewares []middlewareRegistration[T]
}

// NewBuilder constructs one same-port exchange builder.
func NewBuilder[T any](port Port[T]) *Builder[T] {
	return &Builder[T]{port: port}
}

// Port returns the port owned by this builder.
func (b *Builder[T]) Port() Port[T] {
	if b == nil {
		return Port[T]{}
	}
	return b.port
}

// Link registers one same-port link for the builder port.
func (b *Builder[T]) Link(id LinkID, step Step[T, T], opts ...LinkOption[T, T]) *Builder[T] {
	if b == nil {
		return nil
	}
	link := NewLink(id, b.port, b.port, step, opts...)
	b.links = append(b.links, link)
	return b
}

// Use registers one middleware wrapper for the builder port.
func (b *Builder[T]) Use(middleware Middleware[T, T], when ...Predicate) *Builder[T] {
	if b == nil {
		return nil
	}
	b.middlewares = append(b.middlewares, middlewareRegistration[T]{
		middleware: middleware,
		when:       append([]Predicate(nil), when...),
	})
	return b
}

// Build composes the active same-port links and middleware around the provided
// terminal step.
func (b Builder[T]) Build(ctx Context, base Step[T, T]) (Step[T, T], error) {
	if base == nil {
		return nil, fmt.Errorf("exchange builder base step is required")
	}
	links, err := b.activeLinks(ctx)
	if err != nil {
		return nil, err
	}
	step := base
	for i := len(links) - 1; i >= 0; i-- {
		step = composeLink(links[i], step)
	}
	for i := len(b.middlewares) - 1; i >= 0; i-- {
		reg := b.middlewares[i]
		if !predicatesPass(ctx, reg.when) {
			continue
		}
		if reg.middleware == nil {
			return nil, fmt.Errorf("exchange builder middleware %d is required", i)
		}
		step = reg.middleware(step)
	}
	return step, nil
}

func (b Builder[T]) activeLinks(ctx Context) ([]Link[T, T], error) {
	links := make([]Link[T, T], 0, len(b.links))
	for _, link := range b.links {
		if !predicatesPass(ctx, link.When) {
			continue
		}
		links = append(links, link)
	}
	if len(links) == 0 {
		return nil, nil
	}
	return sortLinksByDependency(links)
}

func predicatesPass(ctx Context, predicates []Predicate) bool {
	for _, predicate := range predicates {
		if predicate == nil {
			continue
		}
		if !predicate(ctx) {
			return false
		}
	}
	return true
}

func sortLinksByDependency[T any](links []Link[T, T]) ([]Link[T, T], error) {
	byID := make(map[LinkID]Link[T, T], len(links))
	inDegree := make(map[LinkID]int, len(links))
	outgoing := make(map[LinkID][]LinkID, len(links))
	for _, link := range links {
		if _, exists := byID[link.ID]; exists {
			return nil, fmt.Errorf("exchange builder duplicate link %q", link.ID)
		}
		byID[link.ID] = link
		inDegree[link.ID] = 0
	}
	addEdge := func(from LinkID, to LinkID) error {
		if from == to {
			return fmt.Errorf("exchange builder link cycle detected")
		}
		if _, ok := byID[from]; !ok {
			return nil
		}
		if _, ok := byID[to]; !ok {
			return nil
		}
		outgoing[from] = append(outgoing[from], to)
		inDegree[to]++
		return nil
	}
	for _, link := range links {
		for _, dep := range link.After {
			if err := addEdge(dep, link.ID); err != nil {
				return nil, err
			}
		}
		for _, dep := range link.Before {
			if err := addEdge(link.ID, dep); err != nil {
				return nil, err
			}
		}
	}
	available := make([]LinkID, 0, len(links))
	for id, degree := range inDegree {
		if degree == 0 {
			available = append(available, id)
		}
	}
	sort.Slice(available, func(i, j int) bool {
		return available[i] < available[j]
	})
	orderedIDs := make([]LinkID, 0, len(links))
	for len(available) > 0 {
		id := available[0]
		available = available[1:]
		orderedIDs = append(orderedIDs, id)
		nextIDs := append([]LinkID(nil), outgoing[id]...)
		sort.Slice(nextIDs, func(i, j int) bool {
			return nextIDs[i] < nextIDs[j]
		})
		for _, nextID := range nextIDs {
			inDegree[nextID]--
			if inDegree[nextID] == 0 {
				available = append(available, nextID)
			}
		}
		sort.Slice(available, func(i, j int) bool {
			return available[i] < available[j]
		})
	}
	if len(orderedIDs) != len(links) {
		return nil, fmt.Errorf("exchange builder link cycle detected")
	}
	ordered := make([]Link[T, T], 0, len(links))
	for _, id := range orderedIDs {
		ordered = append(ordered, byID[id])
	}
	return ordered, nil
}

func composeLink[T any](link Link[T, T], next Step[T, T]) Step[T, T] {
	return func(ctx context.Context, input T) (Result[T], error) {
		current, err := link.Run(ctx, input)
		if err != nil {
			return Result[T]{}, err
		}
		following, err := next(ctx, current.Value)
		if err != nil {
			return Result[T]{}, err
		}
		effects := append([]effect.Effect(nil), current.Effects...)
		effects = append(effects, following.Effects...)
		return Result[T]{
			Value:   following.Value,
			Effects: effects,
		}, nil
	}
}
