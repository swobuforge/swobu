package exchange

import (
	"context"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/effect"
)

type markerEffect string

func (e markerEffect) Kind() effect.Kind { return effect.KindObservation }

func TestBuilder_BuildWrapsMiddlewareInRegistrationOrder(t *testing.T) {
	port := NewPort[string](PortID("semantic.request"))
	builder := NewBuilder(port)
	builder.Link("normalize", func(_ context.Context, input string) (Result[string], error) {
		return NewResult(input+"|link", markerEffect("link")), nil
	})
	builder.Use(func(next Step[string, string]) Step[string, string] {
		return func(ctx context.Context, input string) (Result[string], error) {
			res, err := next(ctx, input+"|mw1-in")
			if err != nil {
				return Result[string]{}, err
			}
			res.Value += "|mw1-out"
			res.Effects = append([]effect.Effect{markerEffect("mw1")}, res.Effects...)
			return res, nil
		}
	})
	builder.Use(func(next Step[string, string]) Step[string, string] {
		return func(ctx context.Context, input string) (Result[string], error) {
			res, err := next(ctx, input+"|mw2-in")
			if err != nil {
				return Result[string]{}, err
			}
			res.Value += "|mw2-out"
			res.Effects = append([]effect.Effect{markerEffect("mw2")}, res.Effects...)
			return res, nil
		}
	})

	base := func(_ context.Context, input string) (Result[string], error) {
		return NewResult(input+"|base", markerEffect("base")), nil
	}

	step, err := builder.Build(Context{}, base)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	got, err := step(context.Background(), "start")
	if err != nil {
		t.Fatalf("step() error = %v", err)
	}
	if got.Value != "start|mw1-in|mw2-in|link|base|mw2-out|mw1-out" {
		t.Fatalf("value = %q, want %q", got.Value, "start|mw1-in|mw2-in|link|base|mw2-out|mw1-out")
	}
	if gotEffects := markerEffectIDs(got.Effects); strings.Join(gotEffects, ",") != "mw1,mw2,link,base" {
		t.Fatalf("effects = %v, want %v", gotEffects, []string{"mw1", "mw2", "link", "base"})
	}
}

func TestBuilder_BuildRejectsLinkCycles(t *testing.T) {
	port := NewPort[string](PortID("semantic.request"))
	builder := NewBuilder(port)
	builder.Link("normalize.a", func(context.Context, string) (Result[string], error) {
		return NewResult("a", markerEffect("a")), nil
	}, After[string, string]("normalize.b"))
	builder.Link("normalize.b", func(context.Context, string) (Result[string], error) {
		return NewResult("b", markerEffect("b")), nil
	}, After[string, string]("normalize.a"))

	_, err := builder.Build(Context{}, func(_ context.Context, input string) (Result[string], error) {
		return NewResult(input, markerEffect("base")), nil
	})
	if err == nil {
		t.Fatal("Build() error = nil, want cycle detection failure")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %v, want cycle detection failure", err)
	}
}

func markerEffectIDs(effects []effect.Effect) []string {
	out := make([]string, 0, len(effects))
	for _, eff := range effects {
		if marker, ok := eff.(markerEffect); ok {
			out = append(out, string(marker))
		}
	}
	return out
}
