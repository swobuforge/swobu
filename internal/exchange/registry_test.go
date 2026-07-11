package exchange

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestRegistryBuild_SelectsLowestCostPathAndAppliesSamePortLinksOnce(t *testing.T) {
	start := NewPort[string](PortID("semantic.request"))
	mid := NewPort[int](PortID("wire.document"))
	alt := NewPort[int](PortID("wire.alt_document"))
	goal := NewPort[string](PortID("semantic.response"))

	reg := NewRegistry()
	RegisterLink(reg, "start.normalize", start, start, func(_ context.Context, input string) (Result[string], error) {
		return NewResult(input+"|start", markerEffect("start")), nil
	})
	RegisterLink(reg, "start.to_mid", start, mid, func(_ context.Context, input string) (Result[int], error) {
		return NewResult(len(input), markerEffect("to_mid")), nil
	}, Cost[string, int](1))
	RegisterLink(reg, "mid.normalize", mid, mid, func(_ context.Context, input int) (Result[int], error) {
		return NewResult(input+1, markerEffect("mid")), nil
	})
	RegisterLink(reg, "mid.to_goal", mid, goal, func(_ context.Context, input int) (Result[string], error) {
		return NewResult(fmt.Sprintf("selected:%d", input), markerEffect("to_goal")), nil
	}, Cost[int, string](1))
	RegisterLink(reg, "start.to_alt", start, alt, func(_ context.Context, input string) (Result[int], error) {
		return NewResult(len(input)+10, markerEffect("alt_path")), nil
	}, Cost[string, int](2))
	RegisterLink(reg, "alt.to_goal", alt, goal, func(_ context.Context, input int) (Result[string], error) {
		return NewResult(fmt.Sprintf("alt:%d", input), markerEffect("alt_goal")), nil
	}, Cost[int, string](2))
	RegisterLink(reg, "goal.normalize", goal, goal, func(_ context.Context, input string) (Result[string], error) {
		return NewResult(input+"|goal", markerEffect("goal")), nil
	})

	step, err := reg.Build(Context{}, start.ID(), goal.ID())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got, err := step(context.Background(), "go")
	if err != nil {
		t.Fatalf("step() error = %v", err)
	}
	if got.Value != "selected:9|goal" {
		t.Fatalf("value = %q, want %q", got.Value, "selected:9|goal")
	}
	if gotEffects := markerEffectIDs(got.Effects); strings.Join(gotEffects, ",") != "start,to_mid,mid,to_goal,goal" {
		t.Fatalf("effects = %v, want %v", gotEffects, []string{"start", "to_mid", "mid", "to_goal", "goal"})
	}
}

func TestRegistryBuild_FiltersPredicatesBeforeSearch(t *testing.T) {
	start := NewPort[string](PortID("semantic.request"))
	mid := NewPort[int](PortID("wire.document"))
	goal := NewPort[string](PortID("semantic.response"))

	reg := NewRegistry()
	RegisterLink(reg, "start.to_mid", start, mid, func(_ context.Context, input string) (Result[int], error) {
		return NewResult(len(input), markerEffect("blocked")), nil
	}, Cost[string, int](1), When[string, int](func(Context) bool { return false }))
	RegisterLink(reg, "start.to_goal", start, goal, func(_ context.Context, input string) (Result[string], error) {
		return NewResult(input+"|direct", markerEffect("direct")), nil
	}, Cost[string, string](1))

	step, err := reg.Build(Context{}, start.ID(), goal.ID())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got, err := step(context.Background(), "go")
	if err != nil {
		t.Fatalf("step() error = %v", err)
	}
	if got.Value != "go|direct" {
		t.Fatalf("value = %q, want %q", got.Value, "go|direct")
	}
	if gotEffects := markerEffectIDs(got.Effects); strings.Join(gotEffects, ",") != "direct" {
		t.Fatalf("effects = %v, want %v", gotEffects, []string{"direct"})
	}
}

func TestRegistryBuild_RejectsAmbiguousShortestPath(t *testing.T) {
	start := NewPort[string](PortID("semantic.request"))
	midA := NewPort[int](PortID("wire.document.a"))
	midB := NewPort[int](PortID("wire.document.b"))
	goal := NewPort[string](PortID("semantic.response"))

	reg := NewRegistry()
	RegisterLink(reg, "start.to_mid_a", start, midA, func(_ context.Context, input string) (Result[int], error) {
		return NewResult(len(input), markerEffect("a")), nil
	}, Cost[string, int](1))
	RegisterLink(reg, "mid_a.to_goal", midA, goal, func(_ context.Context, input int) (Result[string], error) {
		return NewResult(fmt.Sprintf("a:%d", input), markerEffect("a_goal")), nil
	}, Cost[int, string](1))
	RegisterLink(reg, "start.to_mid_b", start, midB, func(_ context.Context, input string) (Result[int], error) {
		return NewResult(len(input)+1, markerEffect("b")), nil
	}, Cost[string, int](1))
	RegisterLink(reg, "mid_b.to_goal", midB, goal, func(_ context.Context, input int) (Result[string], error) {
		return NewResult(fmt.Sprintf("b:%d", input), markerEffect("b_goal")), nil
	}, Cost[int, string](1))

	_, err := reg.Build(Context{}, start.ID(), goal.ID())
	if err == nil {
		t.Fatal("Build() error = nil, want ambiguous path failure")
	}
	if !strings.Contains(err.Error(), "ambiguous_path") {
		t.Fatalf("error = %v, want ambiguous_path", err)
	}
}
