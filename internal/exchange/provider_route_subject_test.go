package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/responsesnative"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestAttributeCanonicalDecisionsToRoutePreservesPreciseSubjects(t *testing.T) {
	target := provider.NewTargetSnapshot("target", "openai", "https://example.test", "credential", protocolkind.Responses, "model", "responses")
	decisions := attributeCanonicalDecisionsToRoute([]compat.Decision{
		{Feature: compat.RequestOutputFormat, Outcome: compat.Exact, Subject: "canonical:request.output_format"},
		{Feature: compat.RequestInstructions, Outcome: compat.Approx, Subject: "responses.instructions"},
	}, target)
	if got, want := decisions[0].Subject, routeDecisionSubject("openai", string(protocolkind.Responses)); got != want {
		t.Fatalf("route subject = %q, want %q", got, want)
	}
	if got := decisions[1].Subject; got != "responses.instructions" {
		t.Fatalf("precise codec subject = %q, want preserved", got)
	}
}

func TestResponsesStateForProtocolExcludesUnownedState(t *testing.T) {
	input, err := responsesnative.NewItems([][]byte{[]byte(`{"type":"message"}`)})
	if err != nil {
		t.Fatal(err)
	}
	state := responsesnative.NewRequestState(input, responsesnative.History{})

	for _, test := range []struct {
		name     string
		protocol protocolkind.ProtocolKind
		wantZero bool
	}{
		{name: "responses", protocol: protocolkind.Responses, wantZero: false},
		{name: "messages", protocol: protocolkind.Messages, wantZero: true},
		{name: "chat completions", protocol: protocolkind.ChatCompletions, wantZero: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := responsesStateForProtocol(state, test.protocol)
			if got.Input().IsZero() != test.wantZero {
				t.Fatalf("Responses input zero = %t, want %t", got.Input().IsZero(), test.wantZero)
			}
		})
	}
}
