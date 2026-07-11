package exchange

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestRoutePlanner_PlanMaterializesModelBeforeProtocolResolution(t *testing.T) {
	_, endpoint := testFallbackEndpoint(t)
	planner := RoutePlanner{
		DeliverySelector: FixedDeliverySelector{},
		Continuation:     canonical.NewContinuationRuntime(nil),
	}

	plan, err := planner.Plan(context.Background(), RoutePlanInput{
		Endpoint:       endpoint,
		ClientDelivery: delivery.BufferedDelivery(),
		Request: canonical.NewCanonicalRequest(canonical.RequestParams{
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "hello"),
			},
		}),
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got := plan.Request.Model(); got != "provider-model-a" {
		t.Fatalf("plan request model = %q, want provider-model-a", got)
	}
	if got := plan.Target.BackendRef; got != "backend-a" {
		t.Fatalf("plan target backend ref = %q, want backend-a", got)
	}
	if len(plan.Attempts) != 2 {
		t.Fatalf("plan attempts len = %d, want 2", len(plan.Attempts))
	}
}
