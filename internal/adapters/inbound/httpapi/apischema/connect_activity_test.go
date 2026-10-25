package apischema

import (
	"encoding/json"
	"testing"
)

func TestConnectActivityContractsOnlyExportMountedOperations(t *testing.T) {
	raw, err := Document()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Stage       string           `json:"x-swobu-contract-stage"`
			Parameters  []map[string]any `json:"parameters"`
			RequestBody any              `json:"requestBody"`
			Responses   map[string]any   `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	implemented := map[string]string{
		"/api/v1/clients": "get", "/api/v1/client-configurations": "post",
		"/api/v1/activity": "get", "/api/v1/activity/export": "get", "/api/v1/activity/operations/{id}": "get",
	}
	for path, method := range implemented {
		op := doc.Paths[path][method]
		if op.Stage != "implemented" || op.Responses["200"] == nil {
			t.Errorf("%s missing mounted contract", path)
		}
	}
	for _, path := range []string{"/api/v1/client-setups", "/api/v1/client-setup-previews", "/api/v1/workspaces/{slug}/model-routes/{route}/verifications"} {
		method := "post"
		if path == "/api/v1/client-setups" {
			method = "get"
		}
		if _, ok := doc.Paths[path][method]; ok {
			t.Errorf("unmounted operation exported: %s %s", method, path)
		}
	}
	if doc.Paths["/api/v1/client-configurations"]["post"].RequestBody == nil {
		t.Fatal("Connect review is not typed")
	}
	for _, schema := range []string{"ActivityProjection", "ActivityOperationDetail", "ActivityExchange", "ActivityMCPOperation", "ClientConfigurationReview", "ClientConfigurationPlan"} {
		if doc.Components.Schemas[schema] == nil {
			t.Errorf("missing %s", schema)
		}
	}
}

func TestRetainedActivityExportContractIsConcreteAndBounded(t *testing.T) {
	raw, err := Document()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Stage      string `json:"x-swobu-contract-stage"`
			Parameters []struct {
				Name     string         `json:"name"`
				Required bool           `json:"required"`
				Schema   map[string]any `json:"schema"`
			} `json:"parameters"`
			Responses map[string]any `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	exp := doc.Paths["/api/v1/activity/export"]["get"]
	if exp.Stage != "implemented" || exp.Responses["200"] == nil {
		t.Fatal("export is not a mounted, typed operation")
	}
	want := map[string]bool{"kind": true, "workspace": true, "format": true, "max_rows": true, "cursor": true, "from": true, "to": true, "principal": true, "result": true}
	for _, param := range exp.Parameters {
		delete(want, param.Name)
		if param.Name == "kind" && !param.Required {
			t.Fatal("export must select one evidence kind")
		}
		if param.Name == "max_rows" && param.Schema["maximum"] != float64(2000) && param.Schema["maximum"] != 2000 {
			t.Fatal("export cap missing")
		}
	}
	if len(want) != 0 {
		t.Fatalf("export query contract incomplete: %v", want)
	}
}
