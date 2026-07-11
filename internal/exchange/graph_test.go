package exchange

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/observation"
)

func TestPortLinkAndResultCarryTypedValueAndEffects(t *testing.T) {
	t.Parallel()

	from := NewPort[string]("exchange.in")
	if from.IsZero() {
		t.Fatal("port should not be zero")
	}
	if got := from.ID(); got != "exchange.in" {
		t.Fatalf("port id = %q, want %q", got, "exchange.in")
	}

	link := NewLink(
		LinkID("exchange.test.link"),
		from,
		NewPort[string]("exchange.out"),
		func(_ context.Context, input string) (Result[string], error) {
			return NewResult(
				input+"-done",
				effect.ObservationEffect{Observation: observation.ObservationRecord{Code: "graph_link"}},
			), nil
		},
	)

	result, err := link.Run(context.Background(), "ping")
	if err != nil {
		t.Fatalf("link.Run error = %v", err)
	}
	if got := result.Value; got != "ping-done" {
		t.Fatalf("result value = %q, want %q", got, "ping-done")
	}
	if len(result.Effects) != 1 {
		t.Fatalf("result effects len = %d, want 1", len(result.Effects))
	}
	if got := result.Effects[0].Kind(); got != effect.KindObservation {
		t.Fatalf("effect kind = %q, want %q", got, effect.KindObservation)
	}
}
