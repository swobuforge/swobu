package target_config

import (
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestPlacementCanChangeOnlyDuringTargetCreation(t *testing.T) {
	target := readmodel.TargetReadModel{ID: "a"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	create := NewTargetConfig("dev", route, nil, nil)
	if !create.CanChangePlacement() {
		t.Fatal("creation with an existing target must offer balance or fallback")
	}
	edit := NewEditTargetConfig("dev", route, target, nil, nil)
	if edit.CanChangePlacement() {
		t.Fatal("target settings editing must not offer placement")
	}
}
