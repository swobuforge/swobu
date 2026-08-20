package target_config

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestPlacementCanChangeDuringCreationAndEditing(t *testing.T) {
	target := readmodel.TargetReadModel{ID: "a"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	create := NewTargetConfig("dev", route, nil, nil)
	if !canChangePlacement(create) {
		t.Fatal("creation with an existing target must offer balance or fallback")
	}
	edit := NewEditTargetConfig("dev", route, target, nil, nil)
	if canChangePlacement(edit) {
		t.Fatal("the route's only target has no alternative placement")
	}
	peer := readmodel.TargetReadModel{ID: "b"}
	edit = NewEditTargetConfig("dev", readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}, {Targets: []readmodel.TargetReadModel{peer}}}}, target, nil, nil)
	if !canChangePlacement(edit) {
		t.Fatal("editing among other targets must offer route placement")
	}
}

func TestEditPlacementOptionsUsePostRemovalCollapsedTopology(t *testing.T) {
	a := readmodel.TargetReadModel{ID: "a"}
	b := readmodel.TargetReadModel{ID: "b"}
	c := readmodel.TargetReadModel{ID: "c"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{
		{Targets: []readmodel.TargetReadModel{a}},
		{Targets: []readmodel.TargetReadModel{b}},
		{Targets: []readmodel.TargetReadModel{c}},
	}}

	got := placementOptions(route, targetConfigModeEdit, b.ID)
	want := []readmodel.PlacementOptionReadModel{
		{Label: "primary", Kind: readmodel.PlacementFallback},
		{Label: "balance with step 1", PeerTargetID: a.ID, Kind: readmodel.PlacementBalance},
		{Label: "fallback 1", PeerTargetID: a.ID, Kind: readmodel.PlacementFallback},
		{Label: "balance with step 2", PeerTargetID: c.ID, Kind: readmodel.PlacementBalance},
		{Label: "fallback 2", PeerTargetID: c.ID, Kind: readmodel.PlacementFallback},
	}
	if len(got) != len(want) {
		t.Fatalf("options = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("option %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestEditPlacementOptionsKeepBalancedTierWhenPeerRemains(t *testing.T) {
	a := readmodel.TargetReadModel{ID: "a"}
	b := readmodel.TargetReadModel{ID: "b"}
	c := readmodel.TargetReadModel{ID: "c"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{
		{Targets: []readmodel.TargetReadModel{a, b}},
		{Targets: []readmodel.TargetReadModel{c}},
	}}

	normalized := routeWithoutTarget(route, b.ID)
	if len(normalized.Tiers) != 2 || len(normalized.Tiers[0].Targets) != 1 || normalized.Tiers[0].Targets[0].ID != a.ID {
		t.Fatalf("normalized balanced route = %#v", normalized)
	}
	got := placementOptions(route, targetConfigModeEdit, b.ID)
	if got[len(got)-1].Label != "fallback 2" || got[len(got)-1].PeerTargetID != c.ID {
		t.Fatalf("options after balanced-member removal = %#v", got)
	}
}

func TestExistingTargetPlacementIsDerivedFromEveryTopologyKind(t *testing.T) {
	a := readmodel.TargetReadModel{ID: "a"}
	b := readmodel.TargetReadModel{ID: "b"}
	c := readmodel.TargetReadModel{ID: "c"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{a, b}}, {Targets: []readmodel.TargetReadModel{c}}}}
	if got := currentPlacementForTarget(route, a.ID); got.Kind != readmodel.PlacementBalance || got.PeerTargetID != "b" {
		t.Fatalf("balanced placement = %#v", got)
	}
	if got := currentPlacementForTarget(route, c.ID); got.Kind != readmodel.PlacementFallback || got.PeerTargetID != "a" {
		t.Fatalf("fallback placement = %#v", got)
	}
	primary := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{a}}, {Targets: []readmodel.TargetReadModel{c}}}}
	if got := currentPlacementForTarget(primary, a.ID); got.PeerTargetID != "" || got.Summary() != "primary" {
		t.Fatalf("primary placement = %#v", got)
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

func TestExistingTargetTailRendersEditableRoutingDecision(t *testing.T) {
	target := readmodel.TargetReadModel{ID: "fallback", Provider: "openai", Model: "gpt-5", ProviderProtocol: "responses", CredentialRef: "env:OPENAI_API_KEY"}
	primary := readmodel.TargetReadModel{ID: "primary"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{primary}}, {Targets: []readmodel.TargetReadModel{target}}}}
	config := NewEditTargetConfig("dev", route, target, nil, nil)

	frame := testkit.RenderMountedTrimmed(t, TargetConfigTail(config), 100, 20)

	if !strings.Contains(frame, "routing") || !strings.Contains(frame, "fallback") || !strings.Contains(frame, "change") {
		t.Fatalf("edit form must render an editable routing decision:\n%s", frame)
	}
}

func TestOnlyTargetEditTailOmitsUselessRoutingDecision(t *testing.T) {
	target := readmodel.TargetReadModel{ID: "only", Provider: "openai", Model: "gpt-5", ProviderProtocol: "responses", CredentialRef: "env:OPENAI_API_KEY"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	config := NewEditTargetConfig("dev", route, target, nil, nil)

	frame := testkit.RenderMountedTrimmed(t, TargetConfigTail(config), 100, 20)

	if strings.Contains(frame, "routing") {
		t.Fatalf("only-target edit must not render a no-op placement control:\n%s", frame)
	}
}
