package sharestate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestWorkspaceAndRouteGrantsCoexistAndRotateExactScope(t *testing.T) {
	store := openTestStore(t)
	workspace := mustWorkspaceSlug(t, "dev")
	route := mustRouteName(t, "coding")
	workspaceGrant, err := store.Issue(workspace, routing.RouteName{}, ExpiryOneDay)
	if err != nil {
		t.Fatal(err)
	}
	routeGrant, err := store.Issue(workspace, route, ExpiryOneDay)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := store.Issue(workspace, routing.RouteName{}, ExpirySevenDays)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Bearer == workspaceGrant.Bearer {
		t.Fatal("issuing the same scope did not rotate its bearer")
	}
	grants := store.ActiveGrants()
	if len(grants) != 2 {
		t.Fatalf("active grants = %d, want 2", len(grants))
	}
	if _, err := store.Authenticate(routeGrant.Bearer); err != nil {
		t.Fatalf("route grant was rotated with workspace scope: %v", err)
	}
	raw, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted["schema_version"] != float64(SchemaVersion) {
		t.Fatalf("schema version = %v, want %d", persisted["schema_version"], SchemaVersion)
	}
	workspaceScopes := 0
	routeScopes := 0
	for _, value := range persisted["grants"].([]any) {
		grant := value.(map[string]any)
		if _, ok := grant["kind"]; ok {
			t.Fatalf("v3 grant persisted kind: %v", grant)
		}
		if _, ok := grant["id"]; ok {
			t.Fatalf("v3 grant persisted id: %v", grant)
		}
		if route, ok := grant["route"]; !ok {
			workspaceScopes++
		} else if route == "coding" {
			routeScopes++
		}
	}
	if workspaceScopes != 1 || routeScopes != 1 {
		t.Fatalf("v3 scopes = workspace:%d route:%d, want one of each", workspaceScopes, routeScopes)
	}
	if err := store.Revoke(workspace, routing.RouteName{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(routeGrant.Bearer); err != nil {
		t.Fatalf("exact workspace revoke removed route grant: %v", err)
	}
}

func TestOpenDecodesV2GrantWithoutEagerRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "share.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	workspace := mustWorkspaceSlug(t, "dev")
	route := mustRouteName(t, "coding")
	want, err := store.Issue(workspace, route, ExpiryNever)
	if err != nil {
		t.Fatal(err)
	}
	wantEndpoint, err := store.EndpointID()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	disk["schema_version"] = float64(2)
	raw, _ = json.MarshalIndent(disk, "", "  ")
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	decoded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.ActiveGrants()
	if len(got) != 1 || got[0].Route != route {
		t.Fatalf("decoded grant = %#v", got)
	}
	if got[0].Bearer != want.Bearer || got[0].ExpiresAt != want.ExpiresAt {
		t.Fatal("migration changed bearer or expiry")
	}
	gotEndpoint, err := decoded.EndpointID()
	if err != nil {
		t.Fatal(err)
	}
	if gotEndpoint != wantEndpoint {
		t.Fatalf("EndpointID changed: %q != %q", gotEndpoint, wantEndpoint)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(persisted) != string(raw) {
		t.Fatal("opening v2 state eagerly rewrote the file")
	}
}

func TestOpenRejectsEmptyRouteInV2WithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "share.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	workspace := mustWorkspaceSlug(t, "dev")
	if _, err := store.Issue(workspace, routing.RouteName{}, ExpiryOneDay); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	disk["schema_version"] = float64(2)
	raw, _ = json.MarshalIndent(disk, "", "  ")
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || err.Error() != "decode v2 grant route: required" {
		t.Fatalf("Open empty-route v2 = %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(raw) {
		t.Fatal("rejected v2 state was mutated")
	}
}

func TestReconcileBindingsRevokesMissingWorkspaceAndRouteOnly(t *testing.T) {
	store := openTestStore(t)
	presentWorkspace := mustWorkspaceSlug(t, "present")
	missingWorkspace := mustWorkspaceSlug(t, "missing")
	presentRoute := mustRouteName(t, "present-route")
	missingRoute := mustRouteName(t, "missing-route")
	presentWorkspaceGrant, err := store.Issue(presentWorkspace, routing.RouteName{}, ExpiryNever)
	if err != nil {
		t.Fatal(err)
	}
	presentRouteGrant, err := store.Issue(presentWorkspace, presentRoute, ExpiryNever)
	if err != nil {
		t.Fatal(err)
	}
	missingWorkspaceGrant, err := store.Issue(missingWorkspace, routing.RouteName{}, ExpiryNever)
	if err != nil {
		t.Fatal(err)
	}
	missingRouteGrant, err := store.Issue(presentWorkspace, missingRoute, ExpiryNever)
	if err != nil {
		t.Fatal(err)
	}

	config := configWithWorkspaceAndRoute(t, presentWorkspace, presentRoute)
	changed, err := store.ReconcileBindings(config)
	if err != nil || !changed {
		t.Fatalf("ReconcileBindings = %v, %v", changed, err)
	}
	for scope, bearer := range map[string]string{"workspace": presentWorkspaceGrant.Bearer, "route": presentRouteGrant.Bearer} {
		if _, err := store.Authenticate(bearer); err != nil {
			t.Fatalf("present %s grant was revoked: %v", scope, err)
		}
	}
	for scope, bearer := range map[string]string{"workspace": missingWorkspaceGrant.Bearer, "route": missingRouteGrant.Bearer} {
		if _, err := store.Authenticate(bearer); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("missing %s grant survived: %v", scope, err)
		}
	}

	reopened, err := Open(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := reopened.ReconcileBindings(config); err != nil || changed {
		t.Fatalf("second ReconcileBindings = %v, %v", changed, err)
	}
	for _, bearer := range []string{missingWorkspaceGrant.Bearer, missingRouteGrant.Bearer} {
		if _, err := reopened.Authenticate(bearer); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("revoked grant returned after reopen: %v", err)
		}
	}
}

func configWithWorkspaceAndRoute(t *testing.T, slug routing.WorkspaceSlug, routeName routing.RouteName) routing.Config {
	t.Helper()
	provider, err := routing.ParseProvider("openai", func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	connection, err := routing.NewStandardConnection(provider, "https://example.com", "env:OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := routing.ParseTargetID("primary")
	if err != nil {
		t.Fatal(err)
	}
	model, err := routing.ParseUpstreamModel("model")
	if err != nil {
		t.Fatal(err)
	}
	protocol, err := routing.ParseProtocol("responses", provider, func(routing.Provider, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	target, err := routing.NewTarget(targetID, model, protocol, connection)
	if err != nil {
		t.Fatal(err)
	}
	tier, err := routing.NewTier([]routing.Target{target})
	if err != nil {
		t.Fatal(err)
	}
	route, err := routing.NewRoute(routeName, []routing.Tier{tier})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := routing.NewWorkspace(slug, routeName, []routing.Route{route})
	if err != nil {
		t.Fatal(err)
	}
	config, err := routing.NewConfig([]routing.Workspace{workspace})
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "share.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	return store
}

func mustWorkspaceSlug(t *testing.T, raw string) routing.WorkspaceSlug {
	t.Helper()
	value, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustRouteName(t *testing.T, raw string) routing.RouteName {
	t.Helper()
	value, err := routing.ParseRouteName(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
