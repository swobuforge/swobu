package exchange

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/swobuforge/swobu/internal/effect"
)

// Registry owns cross-port link registration and path selection.
type Registry struct {
	links []Link[any, any]
}

// NewRegistry constructs an empty exchange registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// RegisterLink registers one typed link with the registry.
func RegisterLink[I any, O any](r *Registry, id LinkID, from Port[I], to Port[O], step Step[I, O], opts ...LinkOption[I, O]) *Registry {
	if r == nil {
		return nil
	}
	typed := NewLink(id, from, to, step, opts...)
	r.links = append(r.links, eraseLink(typed))
	return r
}

// Build resolves the lowest-cost path from start to goal and returns one
// executable step that applies same-port normalization once per visited port.
func (r Registry) Build(ctx Context, start PortID, goal PortID) (Step[any, any], error) {
	active := make([]Link[any, any], 0, len(r.links))
	samePort := map[PortID][]Link[any, any]{}
	for _, link := range r.links {
		if !predicatesPass(ctx, link.When) {
			continue
		}
		if link.Step == nil {
			return nil, fmt.Errorf("exchange registry link %q is required", link.ID)
		}
		if link.Cost < 0 {
			return nil, fmt.Errorf("exchange registry link %q has negative cost", link.ID)
		}
		if link.From.IsZero() || link.To.IsZero() {
			return nil, fmt.Errorf("exchange registry link %q requires ports", link.ID)
		}
		if link.From.ID() == link.To.ID() {
			samePort[link.From.ID()] = append(samePort[link.From.ID()], link)
			continue
		}
		active = append(active, link)
	}

	if start == "" || goal == "" {
		return nil, fmt.Errorf("exchange registry start and goal ports are required")
	}

	if len(active) == 0 && start != goal {
		return nil, fmt.Errorf("exchange registry path could not be resolved")
	}

	path, err := shortestPath(start, goal, active)
	if err != nil {
		return nil, err
	}

	portSamePort := make(map[PortID][]Link[any, any], len(samePort))
	for portID, links := range samePort {
		ordered, orderErr := sortLinksByDependency[any](links)
		if orderErr != nil {
			return nil, orderErr
		}
		portSamePort[portID] = ordered
	}

	return func(execCtx context.Context, input any) (Result[any], error) {
		current := NewResult(input)
		var stepErr error
		current, stepErr = applySamePortLinks(execCtx, start, current, portSamePort)
		if stepErr != nil {
			return Result[any]{}, stepErr
		}
		for _, link := range path {
			next, err := link.Run(execCtx, current.Value)
			if err != nil {
				return Result[any]{}, err
			}
			current.Effects = append(current.Effects, next.Effects...)
			current.Value = next.Value
			current, stepErr = applySamePortLinks(execCtx, link.To.ID(), current, portSamePort)
			if stepErr != nil {
				return Result[any]{}, stepErr
			}
		}
		return current, nil
	}, nil
}

func eraseLink[I any, O any](link Link[I, O]) Link[any, any] {
	return Link[any, any]{
		ID:     link.ID,
		From:   NewPort[any](link.From.ID()),
		To:     NewPort[any](link.To.ID()),
		Step:   eraseStep(link),
		When:   append([]Predicate(nil), link.When...),
		After:  append([]LinkID(nil), link.After...),
		Before: append([]LinkID(nil), link.Before...),
		Cost:   link.Cost,
		Tags:   append([]Tag(nil), link.Tags...),
	}
}

func eraseStep[I any, O any](link Link[I, O]) Step[any, any] {
	return func(ctx context.Context, input any) (Result[any], error) {
		if link.Step == nil {
			return Result[any]{}, fmt.Errorf("exchange registry link %q is required", link.ID)
		}
		typedInput, ok := input.(I)
		if !ok {
			var zero I
			return Result[any]{}, fmt.Errorf("exchange registry link %q expected input type %T, got %T", link.ID, zero, input)
		}
		out, err := link.Step(ctx, typedInput)
		if err != nil {
			return Result[any]{}, err
		}
		return Result[any]{
			Value:   any(out.Value),
			Effects: append([]effect.Effect(nil), out.Effects...),
		}, nil
	}
}

func applySamePortLinks(ctx context.Context, port PortID, current Result[any], links map[PortID][]Link[any, any]) (Result[any], error) {
	ordered := links[port]
	if len(ordered) == 0 {
		return current, nil
	}
	next := current
	for _, link := range ordered {
		out, err := link.Run(ctx, next.Value)
		if err != nil {
			return Result[any]{}, err
		}
		next.Effects = append(next.Effects, out.Effects...)
		next.Value = out.Value
	}
	return next, nil
}

func shortestPath(start PortID, goal PortID, links []Link[any, any]) ([]Link[any, any], error) {
	if start == goal {
		return nil, nil
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("exchange registry path could not be resolved")
	}
	nodes := map[PortID]struct{}{
		start: {},
		goal:  {},
	}
	adjacency := map[PortID][]Link[any, any]{}
	for _, link := range links {
		adjacency[link.From.ID()] = append(adjacency[link.From.ID()], link)
		nodes[link.From.ID()] = struct{}{}
		nodes[link.To.ID()] = struct{}{}
	}
	for portID := range adjacency {
		sort.Slice(adjacency[portID], func(i, j int) bool {
			return adjacency[portID][i].ID < adjacency[portID][j].ID
		})
	}

	dist := make(map[PortID]int, len(nodes))
	visited := make(map[PortID]bool, len(nodes))
	parents := make(map[PortID][]Link[any, any], len(nodes))
	for node := range nodes {
		dist[node] = math.MaxInt
	}
	dist[start] = 0

	for len(visited) < len(nodes) {
		current, currentDist := nextUnvisitedNode(dist, visited)
		if currentDist == math.MaxInt {
			break
		}
		visited[current] = true
		for _, link := range adjacency[current] {
			candidate := currentDist + link.Cost
			if candidate < dist[link.To.ID()] {
				dist[link.To.ID()] = candidate
				parents[link.To.ID()] = []Link[any, any]{link}
			} else if candidate == dist[link.To.ID()] {
				parents[link.To.ID()] = append(parents[link.To.ID()], link)
			}
		}
	}
	if dist[goal] == math.MaxInt {
		return nil, fmt.Errorf("exchange registry path could not be resolved")
	}

	for portID := range parents {
		sort.Slice(parents[portID], func(i, j int) bool {
			return parents[portID][i].ID < parents[portID][j].ID
		})
	}

	paths, err := enumerateShortestPaths(start, goal, parents, nil, nil, 2)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("exchange registry path could not be resolved")
	}
	if len(paths) > 1 {
		return nil, fmt.Errorf("ambiguous_path")
	}
	return paths[0], nil
}

func nextUnvisitedNode(dist map[PortID]int, visited map[PortID]bool) (PortID, int) {
	nextID := PortID("")
	nextDist := math.MaxInt
	for node, candidate := range dist {
		if visited[node] {
			continue
		}
		if candidate < nextDist || (candidate == nextDist && node < nextID) {
			nextID = node
			nextDist = candidate
		}
	}
	return nextID, nextDist
}

func enumerateShortestPaths(start PortID, goal PortID, parents map[PortID][]Link[any, any], visiting map[PortID]struct{}, path []Link[any, any], limit int) ([][]Link[any, any], error) {
	if limit <= 0 {
		return nil, nil
	}
	if goal == start {
		return [][]Link[any, any]{append([]Link[any, any](nil), path...)}, nil
	}
	if visiting == nil {
		visiting = map[PortID]struct{}{}
	}
	if _, seen := visiting[goal]; seen {
		return nil, nil
	}
	visiting[goal] = struct{}{}
	defer delete(visiting, goal)

	links := parents[goal]
	if len(links) == 0 {
		return nil, nil
	}
	out := make([][]Link[any, any], 0, limit)
	for _, link := range links {
		nextPath := append([]Link[any, any]{link}, path...)
		subpaths, err := enumerateShortestPaths(start, link.From.ID(), parents, visiting, nextPath, limit-len(out))
		if err != nil {
			return nil, err
		}
		out = append(out, subpaths...)
		if len(out) >= limit {
			return out, nil
		}
	}
	return out, nil
}
