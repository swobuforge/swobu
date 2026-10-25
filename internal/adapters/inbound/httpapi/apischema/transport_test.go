package apischema

import (
	"encoding/json"
	"testing"
)

// These five entrypoints were previously published as target skeletons even
// though their HTTP handlers were already mounted. Their wire and admission
// contracts must remain explicit when adding any subsequent Control resource.
func TestMountedTransportContractsAreConcrete(t *testing.T) {
	raw, err := Document()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]struct {
			Stage       string `json:"x-swobu-contract-stage"`
			Description string `json:"description"`
			Parameters  []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"parameters"`
			Responses map[string]struct {
				Content map[string]any `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		path, method, success, media string
	}{
		{"/api/v1/browser-launches", "post", "201", "application/json"},
		{"/api/v1/browser-session", "post", "204", ""},
		{"/api/v1/activity/stream", "get", "200", "text/event-stream"},
		{"/api/v1/principals/{id}", "delete", "200", "application/json"},
		{"/api/v1/workspaces/{slug}", "delete", "200", "application/json"},
	} {
		op := document.Paths[tt.path][tt.method]
		if op.Stage != "implemented" || op.Description == "" {
			t.Errorf("missing mounted transport contract %s %s", tt.method, tt.path)
			continue
		}
		success, ok := op.Responses[tt.success]
		if !ok {
			t.Errorf("missing actual success status %s %s %s", tt.method, tt.path, tt.success)
			continue
		}
		if tt.media != "" && success.Content[tt.media] == nil {
			t.Errorf("missing content type %s for %s %s", tt.media, tt.method, tt.path)
		}
	}
	principalDelete := document.Paths["/api/v1/principals/{id}"]["delete"]
	ifMatch := false
	for _, parameter := range principalDelete.Parameters {
		if parameter.Name == "If-Match" && parameter.Required {
			ifMatch = true
		}
	}
	if !ifMatch {
		t.Fatal("Principal deletion needs an exact ETag, even for preview")
	}
	workspaceDelete := document.Paths["/api/v1/workspaces/{slug}"]["delete"]
	for _, parameter := range workspaceDelete.Parameters {
		if parameter.Name == "If-Match" && parameter.Required {
			t.Fatal("Workspace deletion preview cannot require an ETag before it is obtained")
		}
	}
}
