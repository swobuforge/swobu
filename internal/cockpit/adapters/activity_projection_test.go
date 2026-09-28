package adapters

import (
	"testing"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestActivityProjectionPreservesResolvedProviderAndModel(t *testing.T) {
	row := activityRowFromExchange(operatorclient.ActivityExchange{
		Route:    "openai",
		Provider: "anthropic",
		Model:    "claude-sonnet-4-6",
	})
	if row.ProviderSpec != "anthropic" || row.ProviderModel != "claude-sonnet-4-6" {
		t.Fatalf("resolved target = %q/%q", row.ProviderSpec, row.ProviderModel)
	}
	if row.RouteID != "openai" {
		t.Fatalf("requested/route evidence = %q/%q", row.RequestedModel, row.RouteID)
	}
}

func TestActivityProjectionFallsBackToHistoricalRouteWithoutUsingResolvedModel(t *testing.T) {
	row := activityRowFromExchange(operatorclient.ActivityExchange{
		Model: "client-model",
		Route: "historical-route",
	})
	if row.RouteID != "historical-route" || row.RouteLabel != "historical-route" {
		t.Fatalf("requested/route projection = %#v", row)
	}
}

func TestActivityProjectionPreservesPendingAndMissingDuration(t *testing.T) {
	row := activityRowFromExchange(operatorclient.ActivityExchange{State: "in_progress"})
	if row.Status != readmodel.ActivityPending {
		t.Fatalf("status = %v, want pending", row.Status)
	}
	if row.DurationKnown {
		t.Fatal("duration known = true, want absent timing preserved")
	}
}

func TestActivityProjectionDistinguishesMeasuredZeroDuration(t *testing.T) {
	zero := 0
	row := activityRowFromExchange(operatorclient.ActivityExchange{State: "succeeded", DurationMS: &zero})
	if !row.DurationKnown || row.Duration != 0 {
		t.Fatalf("measured zero duration = %v known=%v", row.Duration, row.DurationKnown)
	}
}

func TestActivityProjectionRendersFailureWithoutHTTPStatus(t *testing.T) {
	row := activityRowFromExchange(operatorclient.ActivityExchange{State: "failed"})
	if row.Status != readmodel.ActivityFailed || !row.Error || row.HTTPStatus != 0 {
		t.Fatalf("failed exchange without HTTP status projected as %#v", row)
	}
}
