package apischema

import (
	"encoding/json"
	"testing"
)

func TestOperationalLimitsHaveTypedImplementedContract(t *testing.T) {
	raw, err := Document()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Stage       string         `json:"x-swobu-contract-stage"`
			RequestBody any            `json:"requestBody"`
			Responses   map[string]any `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"get", "put"} {
		op := doc.Paths["/api/v1/workspaces/{slug}/service-limits"][method]
		if op.Stage != "implemented" || op.Responses["200"] == nil {
			t.Fatalf("%s operational policy is untyped or not implemented", method)
		}
		if method == "put" && op.RequestBody == nil {
			t.Fatal("missing policy draft")
		}
	}
	if doc.Components.Schemas["WorkspaceServiceLimits"] == nil || doc.Components.Schemas["WorkspaceServiceQuota"] == nil {
		t.Fatal("missing typed limits schema")
	}
}
