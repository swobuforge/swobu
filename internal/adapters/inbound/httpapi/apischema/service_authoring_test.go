package apischema

import (
	"encoding/json"
	"testing"
)

// Shipped Service authoring operations carry concrete contracts for clients.
func TestServiceAuthoringOperationsAreConcrete(t *testing.T) {
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
	for path, methods := range map[string][]string{
		"/api/v1/workspaces/{slug}/model-routes":         {"get"},
		"/api/v1/workspaces/{slug}/model-routes/{route}": {"get", "put", "delete"},
		"/api/v1/workspaces/{slug}/mcp-mounts":           {"get"},
		"/api/v1/workspaces/{slug}/mcp-mounts/{alias}":   {"get", "put", "delete"},
		"/api/v1/workspaces/{slug}/search-route":         {"get", "put", "delete"},
	} {
		for _, method := range methods {
			op, exists := doc.Paths[path][method]
			if !exists || op.Stage != "implemented" {
				t.Errorf("%s %s stage=%q", method, path, op.Stage)
				continue
			}
			if op.Responses["200"] == nil {
				t.Errorf("%s %s lacks success contract", method, path)
			}
			if method == "put" {
				if op.RequestBody == nil {
					t.Errorf("%s %s lacks request body contract", method, path)
				}
			}
		}
	}
	if doc.Paths["/api/v1/workspaces/{slug}/model-routes/{route}"]["delete"].RequestBody != nil || doc.Components.Schemas["DeleteModelRoute"] != nil {
		t.Fatal("route deletion still advertises a replacement selector")
	}
	for _, name := range []string{"ModelRouteSpec", "MCPMount", "MCPMountPreview", "MCPMountReview", "SearchRoute"} {
		if doc.Components.Schemas[name] == nil {
			t.Errorf("missing schema %s", name)
		}
	}
	// Unmounted verification plans belong to the backlog, not exported clients.
	if _, ok := doc.Paths["/api/v1/workspaces/{slug}/model-routes/{route}/verifications"]; ok {
		t.Fatal("unmounted route verification advertised as callable")
	}
}

// Route save is an ordinary Workspace edit: it needs a strong revision, not
// an independently retained replay identity or special response envelope.
func TestModelRouteCommitUsesWorkspaceProjectionWithoutReplay(t *testing.T) {
	raw, err := Document()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Ref string `json:"$ref"`
			} `json:"parameters"`
			Responses map[string]struct {
				Content map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					}
				}
			}
		}
		Components struct{ Schemas map[string]any }
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	put := doc.Paths["/api/v1/workspaces/{slug}/model-routes/{route}"]["put"]
	response := put.Responses["200"].Content["application/json"].Schema.Ref
	if response != "#/components/schemas/WorkspaceProjection" {
		t.Fatal("route writer has parallel response state", response)
	}
	if _, ok := doc.Components.Schemas["ModelRouteCommit"]; ok {
		t.Fatal("retired replay schema remains")
	}
	for _, header := range put.Parameters {
		if header.Ref == "#/components/parameters/IdempotencyKey" {
			t.Fatal("route retry identity remains")
		}
	}
}

func TestMCPMountHasOneRevisionCheckedPutWithoutReplay(t *testing.T) {
	raw, err := Document()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths      map[string]map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/workspaces/{slug}/mcp-mounts"
	if _, ok := doc.Paths[base]["post"]; ok {
		t.Fatal("duplicate POST create path survived")
	}
	mount := doc.Paths[base+"/{alias}"]
	if _, ok := mount["post"]; ok {
		t.Fatal("alias-addressed mount still accepts POST")
	}
	put, ok := mount["put"].(map[string]any)
	if !ok {
		t.Fatal("PUT mount is absent")
	}
	if _, ok := doc.Components.Schemas["MCPMountCommit"]; ok {
		t.Fatal("MCP replay acknowledgement schema survived")
	}
	hasRevision := false
	for _, raw := range put["parameters"].([]any) {
		param := raw.(map[string]any)
		if param["$ref"] == "#/components/parameters/IdempotencyKey" {
			t.Fatal("mount retained independent replay key")
		}
		if param["name"] == "If-Match" {
			if param["required"] != false {
				t.Fatal("preview incorrectly requires a mutation ETag")
			}
			hasRevision = true
		}
	}
	if !hasRevision {
		t.Fatal("mount mutation lacks Workspace revision")
	}
	response := put["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	branches := response["oneOf"].([]any)
	foundWorkspace := false
	for _, raw := range branches {
		if raw.(map[string]any)["$ref"] == "#/components/schemas/WorkspaceProjection" {
			foundWorkspace = true
		}
	}
	if !foundWorkspace {
		t.Fatal("mount apply does not return current Workspace")
	}
}
