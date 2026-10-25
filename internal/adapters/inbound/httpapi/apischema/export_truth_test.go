package apischema

import (
	"encoding/json"
	"testing"
)

// The export is a description of callable Node control, not a roadmap. A
// speculative path can make a generated client compile even though the daemon
// has never mounted a handler for it.
func TestOpenAPIExportsOnlyCurrentControlOperations(t *testing.T) {
	raw, err := Document()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Stage     string         `json:"x-swobu-contract-stage"`
			Responses map[string]any `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	operations := 0
	for path, methods := range doc.Paths {
		if len(methods) == 0 {
			t.Errorf("empty path %s", path)
		}
		for method, op := range methods {
			operations++
			if op.Stage != "implemented" {
				t.Errorf("unqualified exported operation %s %s (%q)", method, path, op.Stage)
			}
			if len(op.Responses) < 2 {
				t.Errorf("exported operation %s %s has no success contract", method, path)
			}
		}
	}
	if operations != 61 {
		t.Fatalf("expected 61 mounted operations, got %d", operations)
	}
	for _, ghost := range []string{
		"/api/v1/workspaces/{slug}/grants", "/api/v1/workspaces/{slug}/grants/{principal_id}",
		"/api/v1/principals/{id}/grants", "/api/v1/node-admin-grants",
		"/api/v1/node-admin-grants/{principal_id}", "/api/v1/node-admin-credentials",
		"/api/v1/workspaces/{slug}/credentials", "/api/v1/workspaces/{slug}/enrollments",
		"/enrollments/{token}/claim", "/api/v1/settings/otel", "/api/v1/client-setups",
	} {
		if _, ok := doc.Paths[ghost]; ok {
			t.Errorf("unmounted target leaked into exported API: %s", ghost)
		}
	}
	for _, live := range []string{
		"/api/v1/workspaces/{slug}/permissions/{id}", "/api/v1/principals/{id}/credentials",
		"/api/v1/activity/stream", "/api/v1/browser-launches", "/api/v1/browser-session",
	} {
		if _, ok := doc.Paths[live]; !ok {
			t.Errorf("live path lost: %s", live)
		}
	}
}
