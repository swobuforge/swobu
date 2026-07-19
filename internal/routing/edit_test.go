package routing

import (
	"errors"
	"testing"
)

func TestRenameDefaultRoutePreservesDefaultIdentity(t *testing.T) {
	config := testConfig(t, testTarget(t, "a"))
	slug, _ := ParseWorkspaceSlug("dev")
	from, _ := ParseRouteName("chat")
	to, _ := ParseRouteName("renamed")
	next, err := config.RenameRoute(slug, from, to)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := next.Workspace(slug)
	if workspace.DefaultRoute() != to {
		t.Fatalf("default = %q", workspace.DefaultRoute().String())
	}
}

func TestCreateTargetBalancesWithPeerOrAppendsFallback(t *testing.T) {
	config := testConfig(t, testTarget(t, "primary"))
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	primaryID, _ := ParseTargetID("primary")

	next, err := config.CreateTarget(slug, routeName, testTarget(t, "balanced"), &primaryID)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := next.Workspace(slug)
	route, _ := workspace.Route(routeName)
	if len(route.Tiers()) != 1 || len(route.Tiers()[0].Targets()) != 2 {
		t.Fatalf("balanced tiers = %#v", route.Tiers())
	}

	next, err = next.CreateTarget(slug, routeName, testTarget(t, "fallback"), nil)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ = next.Workspace(slug)
	route, _ = workspace.Route(routeName)
	if len(route.Tiers()) != 2 || len(route.Tiers()[1].Targets()) != 1 || route.Tiers()[1].Targets()[0].ID().String() != "fallback" {
		t.Fatalf("fallback tiers = %#v", route.Tiers())
	}
}

func TestUpdateTargetSettingsPreservesBalancedTier(t *testing.T) {
	config := testConfig(t, testTarget(t, "one"), testTarget(t, "two"))
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	twoID, _ := ParseTargetID("two")
	two := testTarget(t, "two")
	next, err := config.UpdateTargetSettings(slug, routeName, twoID, TargetSettings{two.Model(), two.Protocol(), two.Connection()})
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := next.Workspace(slug)
	route, _ := workspace.Route(routeName)
	if len(route.Tiers()) != 1 || len(route.Tiers()[0].Targets()) != 2 {
		t.Fatalf("settings update moved target: %#v", route.Tiers())
	}
	ids := []string{route.Tiers()[0].Targets()[0].ID().String(), route.Tiers()[0].Targets()[1].ID().String()}
	if ids[0] != "one" || ids[1] != "two" {
		t.Fatalf("settings update changed balanced peers: %v", ids)
	}
	if got := route.Tiers()[0].Targets()[1].Version(); got != two.Version()+1 {
		t.Fatalf("updated target version = %d, want %d", got, two.Version()+1)
	}
	if got := route.Tiers()[0].Targets()[0].Version(); got != initialTargetVersion {
		t.Fatalf("unrelated target version = %d, want %d", got, initialTargetVersion)
	}
}

func TestDeleteTargetRemovesEmptyTierAndPromotesFallback(t *testing.T) {
	config := testConfig(t, testTarget(t, "primary"))
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	primaryID, _ := ParseTargetID("primary")
	next, err := config.CreateTarget(slug, routeName, testTarget(t, "fallback"), nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err = next.DeleteTarget(slug, routeName, primaryID)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := next.Workspace(slug)
	route, _ := workspace.Route(routeName)
	if len(route.Tiers()) != 1 || route.Tiers()[0].Targets()[0].ID().String() != "fallback" {
		t.Fatalf("tiers = %#v", route.Tiers())
	}
}

func TestDeleteFinalTargetAndRouteAreTypedConflicts(t *testing.T) {
	config := testConfig(t, testTarget(t, "only"))
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	id, _ := ParseTargetID("only")
	if _, err := config.DeleteTarget(slug, routeName, id); !errors.Is(err, ErrLastTarget) {
		t.Fatalf("DeleteTarget error = %v", err)
	}
	if _, err := config.DeleteRoute(slug, routeName, nil); !errors.Is(err, ErrLastRoute) {
		t.Fatalf("DeleteRoute error = %v", err)
	}
}

func TestSetCredentialPreservesUnrelatedTargetFields(t *testing.T) {
	config := testConfig(t, testTarget(t, "a"))
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	id, _ := ParseTargetID("a")
	next, err := config.SetCredential(slug, routeName, id, "env:ROTATED")
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := next.Workspace(slug)
	route, _ := workspace.Route(routeName)
	target := route.Tiers()[0].Targets()[0]
	if target.ID() != id || target.Model().String() != "upstream-a" || target.Protocol().String() != "responses" {
		t.Fatalf("unrelated target fields changed: %#v", target)
	}
	connection := target.Connection().(OpenAIConnection)
	if connection.Credential().String() != "env:ROTATED" {
		t.Fatalf("credential = %q", connection.Credential().String())
	}
	if target.Version() != initialTargetVersion+1 {
		t.Fatalf("credential-save target version = %d, want %d", target.Version(), initialTargetVersion+1)
	}
}

func TestNoOpTargetSavesStillAdvanceVersion(t *testing.T) {
	config := testConfig(t, testTarget(t, "a"))
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	id, _ := ParseTargetID("a")
	workspace, _ := config.Workspace(slug)
	route, _ := workspace.Route(routeName)
	target := route.Tiers()[0].Targets()[0]

	next, err := config.UpdateTargetSettings(slug, routeName, id, TargetSettings{
		Model: target.Model(), Protocol: target.Protocol(), Connection: target.Connection(),
	})
	if err != nil {
		t.Fatal(err)
	}
	nextWorkspace, _ := next.Workspace(slug)
	nextRoute, _ := nextWorkspace.Route(routeName)
	if got := nextRoute.Tiers()[0].Targets()[0].Version(); got != target.Version()+1 {
		t.Fatalf("no-op settings save version = %d, want %d", got, target.Version()+1)
	}

	credential := target.Connection().(OpenAIConnection).Credential().String()
	next, err = next.SetCredential(slug, routeName, id, credential)
	if err != nil {
		t.Fatal(err)
	}
	nextWorkspace, _ = next.Workspace(slug)
	nextRoute, _ = nextWorkspace.Route(routeName)
	if got := nextRoute.Tiers()[0].Targets()[0].Version(); got != target.Version()+2 {
		t.Fatalf("no-op credential save version = %d, want %d", got, target.Version()+2)
	}
}
