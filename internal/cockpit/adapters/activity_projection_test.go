package adapters

import (
	"testing"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
)

func TestActivityProjectionPreservesResolvedProviderAndModel(t *testing.T) {
	row := activityRowFromTraffic(operatorclient.RecentTrafficRow{
		WorkspaceRouteModelID: "openai",
		Route:                 "tgt_opaque:openai",
		ProviderSpec:          "anthropic",
		ProviderModel:         "claude-sonnet-4-6",
	})
	if row.ProviderSpec != "anthropic" || row.ProviderModel != "claude-sonnet-4-6" {
		t.Fatalf("resolved target = %q/%q", row.ProviderSpec, row.ProviderModel)
	}
}
