package adapters

import (
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestPlacementFromReadModelHasOnlyOptionalBalanceTarget(t *testing.T) {
	fallback := placementFromReadModel(readmodel.PlacementOptionReadModel{Kind: readmodel.PlacementFallback})
	if fallback.BalanceWith != nil {
		t.Fatalf("fallback placement = %#v", fallback)
	}
	balance := placementFromReadModel(readmodel.PlacementOptionReadModel{Kind: readmodel.PlacementBalance, PeerTargetID: "a"})
	if balance.BalanceWith == nil || *balance.BalanceWith != "a" {
		t.Fatalf("balance placement = %#v", balance)
	}
}
