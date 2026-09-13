package sharestate

import (
	"encoding/json"
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
