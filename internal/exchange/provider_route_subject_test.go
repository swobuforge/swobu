package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
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
