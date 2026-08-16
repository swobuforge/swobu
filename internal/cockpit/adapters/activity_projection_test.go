package adapters

import (
	"testing"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestActivityProjectionPreservesResolvedProviderAndModel(t *testing.T) {
	row := activityRowFromTraffic(operatorclient.RecentTrafficRow{
		ModelRequested:        "gpt-5.3-codex",
		WorkspaceRouteModelID: "openai",
		Route:                 "tgt_opaque:openai",
		ProviderSpec:          "anthropic",
		ProviderModel:         "claude-sonnet-4-6",
	})
	if row.ProviderSpec != "anthropic" || row.ProviderModel != "claude-sonnet-4-6" {
		t.Fatalf("resolved target = %q/%q", row.ProviderSpec, row.ProviderModel)
	}
	if row.RequestedModel != "gpt-5.3-codex" || row.RouteID != "openai" {
		t.Fatalf("requested/route evidence = %q/%q", row.RequestedModel, row.RouteID)
	}
}

func TestActivityProjectionFallsBackToHistoricalRouteWithoutUsingResolvedModel(t *testing.T) {
	row := activityRowFromTraffic(operatorclient.RecentTrafficRow{
		ModelRequested: "client-model",
		ModelResolved:  "provider-model-must-not-be-route",
		Route:          "historical-route",
	})
	if row.RequestedModel != "client-model" || row.RouteID != "historical-route" || row.RouteLabel != "historical-route" {
		t.Fatalf("requested/route projection = %#v", row)
	}
}

func TestActivityProjectionPreservesPendingAndMissingDuration(t *testing.T) {
	row := activityRowFromTraffic(operatorclient.RecentTrafficRow{Result: "in_progress"})
	if row.Status != readmodel.ActivityPending {
		t.Fatalf("status = %v, want pending", row.Status)
	}
	if row.DurationKnown {
		t.Fatal("duration known = true, want absent timing preserved")
	}
}

func TestActivityProjectionDistinguishesMeasuredZeroDuration(t *testing.T) {
	zero := 0
	row := activityRowFromTraffic(operatorclient.RecentTrafficRow{
		Result: "success",
		Timing: &operatorclient.RecentTrafficTimingRecord{DurMillis: &zero},
	})
	if !row.DurationKnown || row.Duration != time.Duration(0) {
		t.Fatalf("duration = %s known=%v, want measured zero", row.Duration, row.DurationKnown)
	}
}
