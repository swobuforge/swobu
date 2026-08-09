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
	if got := route.Tiers()[0].Targets()[1].Version(); got != two.Version() {
		t.Fatalf("unchanged effective target version = %d, want %d", got, two.Version())
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
	connection := target.Connection().(APIKeyConnection)
	if connection.Credential().String() != "env:ROTATED" {
		t.Fatalf("credential = %q", connection.Credential().String())
	}
	if target.Version() != initialTargetVersion+1 {
		t.Fatalf("credential-save target version = %d, want %d", target.Version(), initialTargetVersion+1)
	}
}

func TestNoOpTargetSavesRetainVersion(t *testing.T) {
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
	if got := nextRoute.Tiers()[0].Targets()[0].Version(); got != target.Version() {
		t.Fatalf("no-op settings save version = %d, want %d", got, target.Version())
	}

	credential := target.Connection().(APIKeyConnection).Credential().String()
	next, err = next.SetCredential(slug, routeName, id, credential)
	if err != nil {
		t.Fatal(err)
	}
	nextWorkspace, _ = next.Workspace(slug)
	nextRoute, _ = nextWorkspace.Route(routeName)
	if got := nextRoute.Tiers()[0].Targets()[0].Version(); got != target.Version() {
		t.Fatalf("no-op credential save version = %d, want %d", got, target.Version())
	}
}

func TestTargetVersionChangesForIndependentTargetSettingsMutations(t *testing.T) {
	openAIBase := testTarget(t, "a")
	modelChanged, _ := ParseUpstreamModel("different-deployment")
	protocolChanged := testProtocol(t, ProviderOpenAI, "chat_completions")
	credentialChanged, _ := NewAPIKeyConnection(ProviderOpenAI, "env:ROTATED_OPENAI_KEY")
	customAuth, _ := NewCustomHeaderAuth("Authorization", "env:CUSTOM_KEY")
	customA, _ := NewCustomConnection("https://custom-a.example/v1", customAuth)
	customB, _ := NewCustomConnection("https://custom-b.example/v1", customAuth)
	regionA, _ := ParseBedrockRegion("eu-west-1")
	regionB, _ := ParseBedrockRegion("eu-west-2")
	bedrockA, _ := NewBedrockConnection(regionA, "https://bedrock-mantle.eu-west-1.api.aws/v1", "")
	bedrockB, _ := NewBedrockConnection(regionB, "https://bedrock-mantle.eu-west-2.api.aws/v1", "")
	targetID, _ := ParseTargetID("a")
	newTarget := func(protocol Protocol, connection Connection) Target {
		t.Helper()
		target, err := NewTarget(targetID, openAIBase.Model(), protocol, connection)
		if err != nil {
			t.Fatal(err)
		}
		return target
	}
	customBase := newTarget(testProtocol(t, ProviderCustom, "responses"), customA)
	bedrockBase := newTarget(testProtocol(t, ProviderBedrock, "responses"), bedrockA)

	cases := []struct {
		name     string
		base     Target
		settings TargetSettings
	}{
		{
			name: "model/deployment within one provider", base: openAIBase,
			settings: TargetSettings{Model: modelChanged, Protocol: openAIBase.Protocol(), Connection: openAIBase.Connection()},
		},
		{
			name: "protocol within one provider", base: openAIBase,
			settings: TargetSettings{Model: openAIBase.Model(), Protocol: protocolChanged, Connection: openAIBase.Connection()},
		},
		{
			name: "custom endpoint within one provider", base: customBase,
			settings: TargetSettings{Model: customBase.Model(), Protocol: customBase.Protocol(), Connection: customB},
		},
		{
			name: "credential reference within one provider", base: openAIBase,
			settings: TargetSettings{Model: openAIBase.Model(), Protocol: openAIBase.Protocol(), Connection: credentialChanged},
		},
		{
			name: "Bedrock region options within one provider", base: bedrockBase,
			settings: TargetSettings{Model: bedrockBase.Model(), Protocol: bedrockBase.Protocol(), Connection: bedrockB},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.base.Provider() != tc.settings.Connection.Provider() {
				t.Fatalf("test setup changed provider from %q to %q", tc.base.Provider(), tc.settings.Connection.Provider())
			}
			config := testConfig(t, tc.base)
			slug, _ := ParseWorkspaceSlug("dev")
			routeName, _ := ParseRouteName("chat")
			id, _ := ParseTargetID("a")
			next, err := config.UpdateTargetSettings(slug, routeName, id, tc.settings)
			if err != nil {
				t.Fatal(err)
			}
			workspace, _ := next.Workspace(slug)
			route, _ := workspace.Route(routeName)
			if got, want := route.Tiers()[0].Targets()[0].Version(), tc.base.Version()+1; got != want {
				t.Fatalf("target version = %d, want %d", got, want)
			}
		})
	}
}

func TestTargetVersionChangesWhenProviderConnectionVariantChanges(t *testing.T) {
	targetID, _ := ParseTargetID("a")
	model, _ := ParseUpstreamModel("shared-deployment")
	region, _ := ParseBedrockRegion("eu-west-1")
	bedrock, _ := NewBedrockConnection(region, "https://bedrock-mantle.eu-west-1.api.aws/anthropic/v1", "")
	protocol := testProtocol(t, ProviderBedrock, "messages")
	base, err := NewTarget(targetID, model, protocol, bedrock)
	if err != nil {
		t.Fatal(err)
	}
	anthropic, _ := NewAPIKeyConnection(ProviderAnthropic, "env:ANTHROPIC_API_KEY")
	anthropicProtocol := testProtocol(t, ProviderAnthropic, "messages")

	config := testConfig(t, base)
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	next, err := config.UpdateTargetSettings(slug, routeName, targetID, TargetSettings{
		Model: model, Protocol: anthropicProtocol, Connection: anthropic,
	})
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := next.Workspace(slug)
	route, _ := workspace.Route(routeName)
	if got, want := route.Tiers()[0].Targets()[0].Version(), base.Version()+1; got != want {
		t.Fatalf("target version = %d, want %d", got, want)
	}
}

func TestTargetRejectsProtocolFromDifferentProvider(t *testing.T) {
	targetID, _ := ParseTargetID("a")
	model, _ := ParseUpstreamModel("shared-deployment")
	bedrockProtocol := testProtocol(t, ProviderBedrock, "messages")
	anthropic, _ := NewAPIKeyConnection(ProviderAnthropic, "env:ANTHROPIC_API_KEY")
	if _, err := NewTarget(targetID, model, bedrockProtocol, anthropic); err == nil {
		t.Fatal("target accepted protocol from a provider that contradicts its connection")
	}
}

func TestUpdateTargetSettingsRejectsProviderChangeWithoutMatchingProtocol(t *testing.T) {
	base := testTarget(t, "a")
	config := testConfig(t, base)
	slug, _ := ParseWorkspaceSlug("dev")
	routeName, _ := ParseRouteName("chat")
	anthropic, _ := NewAPIKeyConnection(ProviderAnthropic, "env:ANTHROPIC_API_KEY")
	if _, err := config.UpdateTargetSettings(slug, routeName, base.ID(), TargetSettings{
		Model: base.Model(), Protocol: base.Protocol(), Connection: anthropic,
	}); err == nil {
		t.Fatal("provider change retained a protocol constructed for the old provider")
	}
}
