package target_config

import (
	"context"
	"errors"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/testkit/cockpittestkit"
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
	b := readmodel.TargetReadModel{ID: "b", Provider: "openai", Model: "gpt", ProviderProtocol: "responses", CredentialRef: "env:OPENAI_API_KEY"}
	c := readmodel.TargetReadModel{ID: "c"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{
		{Targets: []readmodel.TargetReadModel{a}},
		{Targets: []readmodel.TargetReadModel{b}},
		{Targets: []readmodel.TargetReadModel{c}},
	}}

	got := placementOptions(route, targetConfigModeEdit, b.ID)
	want := []readmodel.PlacementOptionReadModel{
		{Label: "primary", Kind: readmodel.PlacementFallback},
		{Label: "balance with primary", PeerTargetID: a.ID, Kind: readmodel.PlacementBalance},
		{Label: "fallback 1", PeerTargetID: a.ID, Kind: readmodel.PlacementFallback},
		{Label: "balance with fallback 1", PeerTargetID: c.ID, Kind: readmodel.PlacementBalance},
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

func TestPlacementReconciliationExcludesMissingAnchors(t *testing.T) {
	a := readmodel.TargetReadModel{ID: "a"}
	b := readmodel.TargetReadModel{ID: "b"}
	c := readmodel.TargetReadModel{ID: "c"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{
		{Targets: []readmodel.TargetReadModel{a}},
		{Targets: []readmodel.TargetReadModel{b}},
	}}

	selected := readmodel.PlacementOptionReadModel{Label: "stale label", PeerTargetID: a.ID, Kind: readmodel.PlacementFallback}
	if got, preserved := reconcilePlacement(route, targetConfigModeEdit, b.ID, selected, true); got.Label != "fallback 1" || got.PeerTargetID != a.ID || !preserved {
		t.Fatalf("valid placement = %#v; want refreshed label with preserved anchor", got)
	}

	refreshed := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{b, c}}}}
	got, preserved := reconcilePlacement(refreshed, targetConfigModeEdit, b.ID, selected, true)
	if got.Kind != readmodel.PlacementBalance || got.PeerTargetID != c.ID || preserved {
		t.Fatalf("edit placement after anchor removal = %#v; want durable balanced placement", got)
	}

	got, preserved = reconcilePlacement(refreshed, targetConfigModeCreate, "", selected, true)
	if got.Kind != readmodel.PlacementFallback || got.PeerTargetID != b.ID || got.Label != "fallback 1" || preserved {
		t.Fatalf("create placement after anchor removal = %#v; want last valid fallback", got)
	}
}

func TestEditRefreshAdoptsDurablePlacementWhenSeededPlacementRemainsExpressible(t *testing.T) {
	a := readmodel.TargetReadModel{ID: "a"}
	b := readmodel.TargetReadModel{ID: "b", Provider: "openai", Model: "gpt", ProviderProtocol: "responses", CredentialRef: "env:OPENAI_API_KEY"}
	original := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{
		{Targets: []readmodel.TargetReadModel{a}},
		{Targets: []readmodel.TargetReadModel{b}},
	}}
	refreshed := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{a, b}}}}
	config := NewEditTargetConfig("dev", original, b, nil, nil)

	config.UpdateTarget("dev", refreshed, b)

	got := config.Placement.Get()
	if got.Kind != readmodel.PlacementBalance || got.PeerTargetID != a.ID {
		t.Fatalf("placement after durable move = %#v; want balance with a", got)
	}
}

func TestEditRefreshPreservesFailedExplicitPlacementWhileItRemainsExpressible(t *testing.T) {
	a := readmodel.TargetReadModel{ID: "a"}
	b := readmodel.TargetReadModel{ID: "b", Provider: "openai", Model: "gpt", ProviderProtocol: "responses", CredentialRef: "env:OPENAI_API_KEY"}
	balanced := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{a, b}}}}
	config := NewEditTargetConfig("dev", balanced, b, func(context.Context, ports.SaveTargetRequest) (ports.SaveTargetResult, error) {
		return ports.SaveTargetResult{}, errors.New("save rejected")
	}, nil)
	explicit := readmodel.PlacementOptionReadModel{Label: "fallback 1", PeerTargetID: a.ID, Kind: readmodel.PlacementFallback}

	config.SelectPlacement(explicit)
	if got := config.Placement.Get(); got.Kind != readmodel.PlacementFallback || got.PeerTargetID != a.ID {
		t.Fatalf("placement after rejected fallback = %#v; want uncommitted fallback after a", got)
	}
	config.UpdateTarget("dev", balanced, b)

	if got := config.Placement.Get(); got.Kind != explicit.Kind || got.PeerTargetID != explicit.PeerTargetID {
		t.Fatalf("placement after failed save and refresh = %#v; want explicit fallback after a", got)
	}
}

func TestEditRefreshReturnsPlacementAuthorityToDurableTopologyAfterDraftAnchorDisappears(t *testing.T) {
	a := readmodel.TargetReadModel{ID: "a"}
	b := readmodel.TargetReadModel{ID: "b", Provider: "openai", Model: "gpt", ProviderProtocol: "responses", CredentialRef: "env:OPENAI_API_KEY"}
	c := readmodel.TargetReadModel{ID: "c"}
	d := readmodel.TargetReadModel{ID: "d"}
	original := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{
		{Targets: []readmodel.TargetReadModel{a}},
		{Targets: []readmodel.TargetReadModel{b, c}},
	}}
	config := NewEditTargetConfig("dev", original, b, func(context.Context, ports.SaveTargetRequest) (ports.SaveTargetResult, error) {
		return ports.SaveTargetResult{}, errors.New("save rejected")
	}, nil)

	config.SelectPlacement(readmodel.PlacementOptionReadModel{Label: "fallback 1", PeerTargetID: a.ID, Kind: readmodel.PlacementFallback})

	withoutDraftAnchor := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{b, c}}}}
	config.UpdateTarget("dev", withoutDraftAnchor, b)
	if got := config.Placement.Get(); got.Kind != readmodel.PlacementBalance || got.PeerTargetID != c.ID {
		t.Fatalf("placement after draft anchor disappeared = %#v; want durable balance with c", got)
	}

	durableMove := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{
		{Targets: []readmodel.TargetReadModel{c}},
		{Targets: []readmodel.TargetReadModel{d}},
		{Targets: []readmodel.TargetReadModel{b}},
	}}
	config.UpdateTarget("dev", durableMove, b)
	if got := config.Placement.Get(); got.Kind != readmodel.PlacementFallback || got.PeerTargetID != d.ID {
		t.Fatalf("placement after second durable move = %#v; want fallback after d", got)
	}
}

func TestSuccessfulPlacementSaveReturnsAuthorityToDurableRefresh(t *testing.T) {
	a := readmodel.TargetReadModel{ID: "a"}
	b := readmodel.TargetReadModel{ID: "b", Provider: "openai", Model: "gpt", ProviderProtocol: "responses", CredentialRef: "env:OPENAI_API_KEY"}
	balanced := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{a, b}}}}
	fallback := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{
		{Targets: []readmodel.TargetReadModel{a}},
		{Targets: []readmodel.TargetReadModel{b}},
	}}
	config := NewEditTargetConfig("dev", balanced, b, func(context.Context, ports.SaveTargetRequest) (ports.SaveTargetResult, error) {
		return ports.SaveTargetResult{Target: b, Route: fallback}, nil
	}, nil)
	explicit := readmodel.PlacementOptionReadModel{Label: "fallback 1", PeerTargetID: a.ID, Kind: readmodel.PlacementFallback}

	config.SelectPlacement(explicit)
	if got := config.Placement.Get(); got.Kind != readmodel.PlacementFallback || got.PeerTargetID != a.ID {
		t.Fatalf("placement after committed fallback = %#v; want committed fallback after a", got)
	}
	config.UpdateTarget("dev", balanced, b)

	got := config.Placement.Get()
	if got.Kind != readmodel.PlacementBalance || got.PeerTargetID != a.ID {
		t.Fatalf("placement after successful save and later durable move = %#v; want balance with a", got)
	}
}

func TestFirstTargetTailOmitsRoutingDecision(t *testing.T) {
	config := NewTargetConfig("dev", readmodel.RouteReadModel{ID: "chat"}, nil, nil)

	frame := testkit.RenderMountedTrimmed(t, TargetConfigTail(config), 100, 12)

	if strings.Contains(frame, "routing") {
		t.Fatalf("first target must not render a placement decision:\n%s", frame)
	}
}

func TestLaterTargetTailRendersRoutingDecision(t *testing.T) {
	target := readmodel.TargetReadModel{ID: "primary"}
	route := readmodel.RouteReadModel{ID: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	config := NewTargetConfig("dev", route, nil, nil)

	frame := testkit.RenderMountedTrimmed(t, TargetConfigTail(config), 100, 12)

	if !strings.Contains(frame, "routing") || !strings.Contains(frame, "fallback 1") {
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

func TestPlacementPickerIsAClosedChoiceWithoutSearch(t *testing.T) {
	config := readyPlacementConfig(t)

	control := PlacementSelect(config)
	harness, err := testkit.NewHarnessAt(control, 100, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	harness.App().FocusNext()
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frame := harness.FrameTrimmed()

	if strings.Contains(frame, "search") {
		t.Fatalf("closed routing placement must not render search:\n%s", frame)
	}
	if strings.Count(frame, "routing") != 1 {
		t.Fatalf("entered placement must have one parent routing label:\n%s", frame)
	}
	if !strings.Contains(frame, "balance with primary") || !strings.Contains(frame, "fallback 1") {
		t.Fatalf("routing placement choices missing:\n%s", frame)
	}
}

func TestPlacementPickerSelectsThroughClosedChoiceGrammar(t *testing.T) {
	config := readyPlacementConfig(t)
	control := PlacementSelect(config)
	harness, err := testkit.NewHarnessAt(control, 100, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	harness.Open()
	harness.App().FocusNext()
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyUp})
	harness.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	if got := config.Placement.Get().Summary(); got != "primary" {
		t.Fatalf("selected placement = %q, want primary", got)
	}
}

func readyPlacementConfig(t *testing.T) *TargetConfig {
	t.Helper()
	config := authoringConfig(t, profile.ProviderSpecOpenAI, "", "env:OPENAI_API_KEY")
	selectReadyModel(config, "gpt-4.1", "responses")
	return config
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
