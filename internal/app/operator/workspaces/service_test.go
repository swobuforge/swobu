package workspaces

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

// Routing publication cannot roll back independently committed revocations.
// Recovery retries the semantic edit, never resurrects old bearer authority.

// Exact-target directory obstruction forces config rename failure after
// the before-commit revocation boundary, independent of permission bits.

func testService(t *testing.T) (Service, *configstore.Store, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service, err := NewService(store, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	return service, store, path
}
func openAITarget(id string) TargetDraft {
	return TargetDraft{ID: id, Model: "gpt-5", Protocol: "responses", ConnectionID: "cn_openai"}
}

func TestNewServiceRejectsNilStore(t *testing.T) {
	if _, err := NewService(nil, Dependencies{}); err == nil {
		t.Fatal("NewService(nil, Dependencies{}) unexpectedly succeeded")
	}
}

func TestRouteSpecRoundTripIsSemanticNoOpAndPreservesEveryVersion(t *testing.T) {
	service, store, _ := testService(t)
	ctx := context.Background()
	if _, _, err := service.CreateWorkspaceChecked(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: openAITarget("a")}, "fixture-operator", "fixture-17405", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	before := routeVersions(t, store.Config(), "dev", "chat")
	spec, err := service.GetRoute(ctx, "dev", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := applyReviewedRouteForTest(t, service, ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: spec}); err != nil {
		t.Fatal(err)
	}
	after := routeVersions(t, store.Config(), "dev", "chat")
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("versions after exact round trip = %#v, want %#v", after, before)
	}
}

func TestConcreteProtocolRouteSpecRoundTripPreservesField(t *testing.T) {
	service, store, _ := testService(t)
	ctx := context.Background()
	target := TargetDraft{ID: "chatgpt", Model: "gpt-5", Protocol: "responses", ConnectionID: "cn_chatgpt"}
	if _, _, err := service.CreateWorkspaceChecked(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: target}, "fixture-operator", "fixture-18295", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	before := routeVersions(t, store.Config(), "dev", "chat")
	spec, err := service.GetRoute(ctx, "dev", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.Tiers[0].Targets[0].Protocol; got != "responses" {
		t.Fatalf("editable protocol = %q, want responses", got)
	}
	if _, err := applyReviewedRouteForTest(t, service, ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: spec}); err != nil {
		t.Fatal(err)
	}
	if after := routeVersions(t, store.Config(), "dev", "chat"); !reflect.DeepEqual(after, before) {
		t.Fatalf("versions after concrete protocol round trip = %#v, want %#v", after, before)
	}
}

func TestModelRouteWriteRejectsStaleSpecification(t *testing.T) {
	service, _, _ := testService(t)
	ctx := t.Context()
	reviewed, _, err := service.CreateWorkspaceChecked(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: openAITarget("a")}, "fixture", "create", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	first, _ := service.GetRoute(ctx, "dev", "chat")
	stale, _ := service.GetRoute(ctx, "dev", "chat")
	first.Tiers[0].Targets[0].Model = "first-writer"
	if _, err := service.PutModelRouteChecked(ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: first}, reviewed.ManagementETag, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	stale.Tiers[0].Targets[0].Model = "last-writer"
	if _, err := service.PutModelRouteChecked(ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: stale}, reviewed.ManagementETag, func(context.Context) error { return nil }); err == nil {
		t.Fatal("stale specification accepted")
	}
	committed, err := service.GetRoute(ctx, "dev", "chat")
	if err != nil || committed.Tiers[0].Targets[0].Model != "first-writer" {
		t.Fatal("stale write changed topology", err)
	}
}

func TestReplaceRouteRejectsOneInvalidTargetWithoutPublishingOtherChanges(t *testing.T) {
	service, store, path := testService(t)
	ctx := context.Background()
	if _, _, err := service.CreateWorkspaceChecked(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: openAITarget("a")}, "fixture-operator", "fixture-20182", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	beforeDisk, _ := os.ReadFile(path)
	before := routeVersions(t, store.Config(), "dev", "chat")
	spec, _ := service.GetRoute(ctx, "dev", "chat")
	spec.Tiers[0].Targets[0].Model = "changed"
	spec.Tiers = append(spec.Tiers, TierSpec{Targets: []TargetDraft{{ID: "broken", Model: "", Protocol: "responses", ConnectionID: "cn_openai"}}})
	if _, err := applyReviewedRouteForTest(t, service, ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: spec}); err == nil {
		t.Fatal("ReplaceRoute unexpectedly succeeded")
	}
	afterDisk, _ := os.ReadFile(path)
	if string(afterDisk) != string(beforeDisk) || !reflect.DeepEqual(routeVersions(t, store.Config(), "dev", "chat"), before) {
		t.Fatal("invalid desired route changed disk or published snapshot")
	}
}

func routeVersions(t *testing.T, config routing.Config, rawWorkspace, rawRoute string) map[string]routing.TargetVersion {
	t.Helper()
	slug, _ := routing.ParseWorkspaceSlug(rawWorkspace)
	name, _ := routing.ParseRouteName(rawRoute)
	workspace, ok := config.Workspace(slug)
	if !ok {
		t.Fatal("workspace missing")
	}
	route, ok := workspace.Route(name)
	if !ok {
		t.Fatal("route missing")
	}
	out := map[string]routing.TargetVersion{}
	for _, tier := range route.Tiers() {
		for _, target := range tier.Targets() {
			out[target.ID().String()] = target.Version()
		}
	}
	return out
}

func TestOperatorTargetDraftRejectsEmptyConnection(t *testing.T) {
	_, err := (TargetDraft{ID: "ambiguous", Model: "model", Protocol: "responses"}).routingTarget()
	if err == nil || !strings.Contains(err.Error(), "Connection ID is required") {
		t.Fatalf("finalize error = %v, want empty connection rejection", err)
	}
}

func TestProjectTargetPreservesEffectiveDerivedProtocol(t *testing.T) {
	for _, test := range []struct {
		name       string
		connection routing.Connection
		protocol   string
	}{
		{name: "Z.AI", connection: mustZAIConnection(t), protocol: routing.ZAIProviderProtocol},
		{name: "DeepSeek", connection: mustDeepSeekConnection(t), protocol: routing.DeepSeekProviderProtocol},
		{name: "Kimi", connection: mustKimiConnection(t), protocol: routing.KimiProviderProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, err := routing.ParseTargetID("target")
			if err != nil {
				t.Fatal(err)
			}
			model, err := routing.ParseUpstreamModel("model")
			if err != nil {
				t.Fatal(err)
			}
			protocol, err := routing.ParseProtocol(test.protocol, test.connection.Provider(), func(routing.Provider, string) bool { return true })
			if err != nil {
				t.Fatal(err)
			}
			target, err := routing.NewTarget(id, model, protocol, mustConnectionID(t, "cn_shared"))
			if err != nil {
				t.Fatal(err)
			}
			if got := projectTarget(target).Protocol; got != test.protocol {
				t.Fatalf("projected protocol = %q, want %q", got, test.protocol)
			}
		})
	}
}

func mustZAIConnection(t *testing.T) routing.Connection {
	t.Helper()
	provider, _ := routing.ParseProvider("zai", profile.SupportsSpec)
	connection, err := routing.NewZAIConnection(provider, routing.ZAIAccessCodingPlan, "env:ZAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func mustDeepSeekConnection(t *testing.T) routing.Connection {
	t.Helper()
	provider, _ := routing.ParseProvider("deepseek", profile.SupportsSpec)
	connection, err := routing.NewStandardConnection(provider, "", "env:DEEPSEEK_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func mustKimiConnection(t *testing.T) routing.Connection {
	t.Helper()
	provider, _ := routing.ParseProvider("kimi", profile.SupportsSpec)
	connection, err := routing.NewStandardConnection(provider, "", "env:MOONSHOT_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func TestModelRouteWriteReturnsCommittedHierarchy(t *testing.T) {
	service, _, _ := testService(t)
	ctx := context.Background()
	workspace, _, err := service.CreateWorkspaceChecked(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: openAITarget("a")}, "fixture-operator", "fixture-28442", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(workspace.Routes) != 1 || len(workspace.Routes[0].Tiers) != 1 {
		t.Fatalf("workspace = %#v", workspace)
	}
	spec, _ := service.GetRoute(ctx, "dev", "chat")
	spec.Tiers = append(spec.Tiers, TierSpec{Targets: []TargetDraft{openAITarget("b")}})
	_, err = applyReviewedRouteForTest(t, service, ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: spec})
	if err != nil {
		t.Fatal(err)
	}
	committed, _ := service.GetRoute(ctx, "dev", "chat")
	if len(committed.Tiers) != 2 {
		t.Fatalf("tiers = %#v", committed.Tiers)
	}
	committed.Tiers[1].Targets = append(committed.Tiers[1].Targets, openAITarget("c"))
	_, err = applyReviewedRouteForTest(t, service, ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: committed})
	if err != nil {
		t.Fatal(err)
	}
	if len(committed.Tiers) != 2 || len(committed.Tiers[1].Targets) != 2 {
		t.Fatalf("balanced tiers = %#v", committed.Tiers)
	}
	committed.Tiers = committed.Tiers[1:]
	_, err = applyReviewedRouteForTest(t, service, ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: committed})
	if err != nil {
		t.Fatal(err)
	}
	committed, _ = service.GetRoute(ctx, "dev", "chat")
	if len(committed.Tiers) != 1 || committed.Tiers[0].Targets[0].ID != "b" {
		t.Fatalf("promoted tiers = %#v", committed.Tiers)
	}
}

func TestConnectionRebindPreservesRouteTierDefaultAndTargetIdentity(t *testing.T) {
	service, store, _ := testService(t)
	ctx := context.Background()
	if _, _, err := service.CreateWorkspaceChecked(ctx, CreateWorkspace{
		Slug: "dev", InitialRoute: "chat", Target: openAITarget("primary"),
	}, "fixture-operator", "fixture-provider-switch", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	initial, err := service.GetRoute(ctx, "dev", "chat")
	if err != nil {
		t.Fatal(err)
	}
	initial.Tiers = append(initial.Tiers, TierSpec{Targets: []TargetDraft{{
		ID: "fallback", Model: "claude-sonnet", Protocol: "responses",
		ConnectionID: "cn_bedrock",
	}}})
	if _, err := applyReviewedRouteForTest(t, service, ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: initial}); err != nil {
		t.Fatal(err)
	}
	withBedrock, err := service.GetWorkspace(ctx, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(withBedrock.Routes) != 1 || len(withBedrock.Routes[0].Tiers) != 2 {
		t.Fatalf("Bedrock route topology = %#v", withBedrock)
	}
	bedrock := withBedrock.Routes[0].Tiers[1].Targets[0]
	if bedrock.ID != "fallback" || bedrock.ConnectionID != "cn_bedrock" || bedrock.Protocol != "responses" {
		t.Fatalf("Bedrock target = %#v", bedrock)
	}
	bedrockVersion := routeVersions(t, store.Config(), "dev", "chat")["fallback"]

	initial.Tiers[1].Targets[0] = TargetDraft{
		ID: "fallback", Model: "openrouter/claude-sonnet", Protocol: "chat_completions",
		ConnectionID: "cn_openrouter",
	}
	if _, err := applyReviewedRouteForTest(t, service, ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: initial}); err != nil {
		t.Fatal(err)
	}
	committed, err := service.GetWorkspace(ctx, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(committed.Routes) != 1 || len(committed.Routes[0].Tiers) != 2 {
		t.Fatalf("switched route topology = %#v", committed)
	}
	primary := committed.Routes[0].Tiers[0].Targets[0]
	fallback := committed.Routes[0].Tiers[1].Targets[0]
	if primary.ID != "primary" || primary.ConnectionID != "cn_openai" || fallback.ID != "fallback" || fallback.ConnectionID != "cn_openrouter" || fallback.Protocol != "chat_completions" {
		t.Fatalf("Connection rebind targets = primary %#v fallback %#v", primary, fallback)
	}
	if got := routeVersions(t, store.Config(), "dev", "chat")["fallback"]; got != bedrockVersion+1 {
		t.Fatalf("switched target generation = %d, want %d", got, bedrockVersion+1)
	}
}

func TestRetainedRouteConnectionRebindPreservesTargetFactsAcrossRestart(t *testing.T) {
	service, store, path := testService(t)
	ctx := context.Background()
	_, _, err := service.CreateWorkspaceChecked(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: openAITarget("a")}, "fixture-operator", "fixture-29999", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := service.GetWorkspace(ctx, "dev")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := service.GetRoute(ctx, "dev", "chat")
	if err != nil {
		t.Fatal(err)
	}
	originalVersion := routeVersions(t, store.Config(), "dev", "chat")["a"]
	spec.Tiers[0].Targets[0].ConnectionID = "cn_rebound"
	workspace, err := service.PutModelRouteChecked(ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: spec}, reviewed.ManagementETag, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	target := workspace.Routes[0].Tiers[0].Targets[0]
	if target.Model != "gpt-5" || target.Protocol != "responses" || target.ConnectionID != "cn_rebound" {
		t.Fatalf("target = %#v", target)
	}
	if len(workspace.Routes[0].Tiers) != 1 || len(workspace.Routes[0].Tiers[0].Targets) != 1 {
		t.Fatalf("Connection rebind changed tier placement: %#v", workspace.Routes[0].Tiers)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, _ := reopened.Config().Workspace(mustSlug(t, "dev"))
	route, _ := persisted.Route(mustRoute(t, "chat"))
	actual := route.Tiers()[0].Targets()[0]
	if actual.ID().String() != "a" || actual.Version() != originalVersion+1 || actual.Model().String() != "gpt-5" || actual.Protocol().String() != "responses" || actual.ConnectionID().String() != "cn_rebound" {
		t.Fatalf("persisted target = %#v", actual)
	}
}

// Credential policies are enforced when constructing the reusable Connection;
// route replacement carries only the stable reference and cannot edit auth.
func TestServiceDeletePrimaryPromotesFallbackAcrossRestart(t *testing.T) {
	service, store, path := testService(t)
	ctx := context.Background()
	if _, _, err := service.CreateWorkspaceChecked(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: openAITarget("primary")}, "fixture-operator", "fixture-33155", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	spec, _ := service.GetRoute(ctx, "dev", "chat")
	spec.Tiers = append(spec.Tiers, TierSpec{Targets: []TargetDraft{openAITarget("fallback")}})
	if _, err := applyReviewedRouteForTest(t, service, ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: spec}); err != nil {
		t.Fatal(err)
	}
	spec.Tiers = spec.Tiers[1:]
	if _, err := applyReviewedRouteForTest(t, service, ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: spec}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := configstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	workspace, ok := reopened.Config().Workspace(mustSlug(t, "dev"))
	if !ok {
		t.Fatal("workspace missing after restart")
	}
	route, ok := workspace.Route(mustRoute(t, "chat"))
	if !ok {
		t.Fatal("route missing after restart")
	}
	tiers := route.Tiers()
	if len(tiers) != 1 || len(tiers[0].Targets()) != 1 || tiers[0].Targets()[0].ID().String() != "fallback" {
		t.Fatalf("restarted primary tier = %#v, want fallback promoted", tiers)
	}
}
func mustSlug(t *testing.T, raw string) routing.WorkspaceSlug {
	t.Helper()
	value, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func mustRoute(t *testing.T, raw string) routing.RouteName {
	t.Helper()
	value, err := routing.ParseRouteName(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestServiceMapsEmptyDesiredRouteAsInvalidArgument(t *testing.T) {
	service, _, _ := testService(t)
	ctx := context.Background()
	if _, _, err := service.CreateWorkspaceChecked(ctx, CreateWorkspace{Slug: "dev", InitialRoute: "chat", Target: openAITarget("a")}, "fixture-operator", "fixture-34814", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	_, err := applyReviewedRouteForTest(t, service, ctx, ReplaceRoute{Workspace: "dev", Route: "chat", Spec: RouteSpec{}})
	var command CommandError
	if !errors.As(err, &command) || command.Code != InvalidArgument {
		t.Fatalf("error = %#v", err)
	}
}

func TestCommandErrorCollapsesDomainConflictSentinels(t *testing.T) {
	for _, sentinel := range []error{
		routing.ErrConflict,
		routing.ErrLastTarget,
	} {
		var command CommandError
		if err := commandError(fmt.Errorf("%w: specific reason", sentinel)); !errors.As(err, &command) || command.Code != Conflict || !strings.Contains(command.Message, "specific reason") {
			t.Errorf("commandError(%v) = %#v", sentinel, err)
		}
	}
}

// applyReviewedRouteForTest supplies fresh explicit review for sequential semantic
// reconciliation proofs. Stale-review/replay proofs call the retained seam directly.
func applyReviewedRouteForTest(t *testing.T, service Service, ctx context.Context, command ReplaceRoute) (Workspace, error) {
	t.Helper()
	reviewed, err := service.GetWorkspace(ctx, command.Workspace)
	if err != nil {
		return Workspace{}, err
	}
	result, err := service.PutModelRouteChecked(ctx, command, reviewed.ManagementETag, func(context.Context) error { return nil })
	return result, err
}

func mustConnectionID(t *testing.T, raw string) connectionidentity.ID {
	t.Helper()
	id, err := connectionidentity.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Invalid source names are not an alternate Workspace attachment format.
// The registry is required even in application commands issued without HTTP.
func TestWorkspaceAttachmentsRequireRealConnections(t *testing.T) {
	service, store, _ := testService(t)
	assertReference := func(raw string, kind connections.Kind, want ErrorCode) {
		t.Helper()
		err := service.validateConnectionReference(raw, kind)
		if want == "" && err == nil {
			return
		}
		var typed CommandError
		if !errors.As(err, &typed) || typed.Code != want {
			t.Fatalf("reference %q as %s = %v, want %s", raw, kind, err, want)
		}
	}

	assertReference("", connections.Search, InvalidArgument)
	assertReference("legacy-source", connections.Search, InvalidArgument)
	assertReference("cn_search", connections.Search, Unavailable)

	registry, err := connectionfixture.Open(filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := connections.ParseEndpoint("https://search.example.test")
	if err != nil {
		t.Fatal(err)
	}
	searchSpec, err := connections.NewSearchSpec("brave", endpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	mcpSpec, err := connections.NewMCPHTTPSpec(endpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		id   string
		name string
		spec connections.Spec
	}{{"cn_search", "Search", searchSpec}, {"cn_mcp", "MCP", mcpSpec}} {
		id, err := connectionidentity.Parse(entry.id)
		if err != nil {
			t.Fatal(err)
		}
		value, err := connections.New(id, entry.name, entry.spec, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if err := registry.Publish(value, connections.Revision{}); err != nil {
			t.Fatal(err)
		}
	}
	service.connections = registry
	assertReference("cn_search", connections.Search, "")
	assertReference("cn_mcp", connections.MCP, "")
	assertReference("cn_search", connections.MCP, InvalidArgument)
	assertReference("cn_mcp", connections.Search, InvalidArgument)
	assertReference("cn_missing", connections.Search, NotFound)
	assertReference("cn_NOT_LOWERCASE", connections.Search, InvalidArgument)
	assertReference(" cn_search ", connections.Search, InvalidArgument)

	created, _, err := service.CreateWorkspaceChecked(t.Context(), CreateWorkspace{Slug: "research"}, "operator", "create-research", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	withSearch, err := service.SetServiceChecked(t.Context(), "research", "search", true, created.ManagementETag, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"legacy-source", "cn_missing", "cn_mcp", "cn_NOT_LOWERCASE", " cn_search ", " "} {
		_, err := service.ChangeSearchRouteChecked(t.Context(), "research", invalid, nil, withSearch.ManagementETag, func(context.Context) error { return nil })
		if err == nil {
			t.Fatalf("invalid Search reference %q published", invalid)
		}
		current, err := store.GetWorkspace(t.Context(), mustSlug(t, "research"))
		if err != nil {
			t.Fatal(err)
		}
		if current.SearchConnectionID() != "" || current.ManagementRevision() != withSearch.ManagementRevision {
			t.Fatalf("invalid Search attachment mutated Workspace: %+v", current)
		}
	}
	for _, invalid := range []string{"", " ", "legacy-source", "cn_missing", "cn_search", "cn_NOT_LOWERCASE", " cn_mcp "} {
		draft := MCPMountDraft{Workspace: "research", Alias: "docs", ConnectionID: invalid, SelectedTools: []string{"read_file"}}
		if _, err := service.PreviewMCPMount(t.Context(), draft); err == nil {
			t.Fatalf("invalid MCP reference %q previewed", invalid)
		}
		if _, err := service.ApplyMCPMountChecked(t.Context(), draft, MCPMountReviewRequest{}, func(context.Context) error { return nil }); err == nil {
			t.Fatalf("invalid MCP reference %q published", invalid)
		}
		current, err := store.GetWorkspace(t.Context(), mustSlug(t, "research"))
		if err != nil {
			t.Fatal(err)
		}
		if len(current.MCPMounts()) != 0 || current.ManagementRevision() != withSearch.ManagementRevision {
			t.Fatalf("invalid MCP attachment mutated Workspace: %+v", current)
		}
	}
	attached, err := service.ChangeSearchRouteChecked(t.Context(), "research", "cn_search", nil, withSearch.ManagementETag, func(context.Context) error { return nil })
	if err != nil || attached.SearchConnectionID != "cn_search" {
		t.Fatalf("canonical Search Connection attach = %+v, %v", attached, err)
	}
}
