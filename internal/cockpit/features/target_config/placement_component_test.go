package target_config

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestPlacementCanChangeOnlyDuringTargetCreation(t *testing.T) {
	target := readmodel.TargetReadModel{ID: "a"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	create := NewTargetConfig("dev", route, nil, nil)
	if !canChangePlacement(create) {
		t.Fatal("creation with an existing target must offer balance or fallback")
	}
	edit := NewEditTargetConfig("dev", route, target, nil, nil)
	if canChangePlacement(edit) {
		t.Fatal("target settings editing must not offer placement")
	}
}

func TestFirstTargetTailOmitsRoutingDecision(t *testing.T) {
	config := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)

	frame := testkit.RenderMountedTrimmed(t, TargetConfigTail(config), 100, 12)

	if strings.Contains(frame, "routing") || strings.Contains(frame, "fallback after step") {
		t.Fatalf("first target must not render a placement decision:\n%s", frame)
	}
}

func TestLaterTargetTailRendersRoutingDecision(t *testing.T) {
	target := readmodel.TargetReadModel{ID: "primary"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	config := NewTargetConfig("dev", route, nil, nil)

	frame := testkit.RenderMountedTrimmed(t, TargetConfigTail(config), 100, 12)

	if !strings.Contains(frame, "routing") || !strings.Contains(frame, "fallback after step 1") {
		t.Fatalf("later target must render its placement decision:\n%s", frame)
	}
}
