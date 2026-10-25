package apischema

import (
	"encoding/json"
	"testing"
)

func TestAuthorityContractsPromoteOnlyMountedReviewedOperations(t *testing.T) {
	raw, err := Document()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]struct {
			Stage     string         `json:"x-swobu-contract-stage"`
			Responses map[string]any `json:"responses"`
		} `json:"paths"`
	}
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ method, path string }{
		{"get", "/api/v1/principals"}, {"post", "/api/v1/principals"}, {"get", "/api/v1/principals/{id}"},
		{"get", "/api/v1/principals/{id}/credentials"}, {"post", "/api/v1/principals/{id}/credentials"},
		{"delete", "/api/v1/principals/{id}/credentials/{credential}"},
		{"get", "/api/v1/workspaces/{slug}/permissions/{id}"}, {"put", "/api/v1/workspaces/{slug}/permissions/{id}"},
		{"delete", "/api/v1/workspaces/{slug}/permissions/{id}"}, {"get", "/api/v1/workspaces/{slug}/limits/status"},
		{"post", "/api/v1/workspaces/{slug}/limits/preview"}, {"post", "/api/v1/workspaces/{slug}/limits/apply"},
		{"delete", "/api/v1/workspaces/{slug}/limits"}, {"get", "/api/v1/remote-addresses"},
		{"get", "/api/v1/workspaces/{slug}/publications"}, {"put", "/api/v1/workspaces/{slug}/publications/{remote_address_id}"}, {"delete", "/api/v1/workspaces/{slug}/publications/{remote_address_id}"},
	} {
		op := document.Paths[tt.path][tt.method]
		if op.Stage != "implemented" || len(op.Responses) < 2 {
			t.Errorf("missing mounted contract %s %s: %+v", tt.method, tt.path, op)
		}
	}
	for _, path := range []string{"/api/v1/workspaces/{slug}/grants", "/api/v1/workspaces/{slug}/grants/{principal_id}", "/api/v1/principals/{id}/grants"} {
		if _, ok := document.Paths[path]; ok {
			t.Errorf("unmounted grant route advertised: %s", path)
		}
	}
}

// Dry-run endpoints intentionally admit reviews without an If-Match or retry
// key; their commits must still require both. This parameter shape is part of
// the browser contract and cannot be inherited blindly from target scaffolds.
func TestAuthorityReviewHeadersAreOptionalInSchema(t *testing.T) {
	raw, err := Document()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name     string `json:"name"`
				In       string `json:"in"`
				Required bool   `json:"required"`
				Ref      string `json:"$ref"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ Path, Method string }{
		{"/api/v1/workspaces/{slug}/permissions/{id}", "put"},
		{"/api/v1/workspaces/{slug}/permissions/{id}", "delete"},
		{"/api/v1/principals/{id}/credentials/{credential}", "delete"},
	} {
		op := doc.Paths[entry.Path][entry.Method]
		for _, param := range op.Parameters {
			if param.Ref == "#/components/parameters/IfMatch" || param.Ref == "#/components/parameters/IdempotencyKey" {
				t.Errorf("review parameter falsely required: %s %s", entry.Method, entry.Path)
			}
			if param.In == "header" && (param.Name == "If-Match" || param.Name == "Idempotency-Key") && param.Required {
				t.Errorf("dry run requires %s on %s %s", param.Name, entry.Method, entry.Path)
			}
		}
	}
}
