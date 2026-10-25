package apischema

// modelRouteContracts promotes the checked canonical Models route lifecycle.
// Legacy POST creation is not promoted; the full-spec PUT replaces it.
func modelRouteContracts(document map[string]any) {
	paths := document["paths"].(map[string]any)
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	success := func(schema any) map[string]any {
		return map[string]any{"description": "Current or committed Models route", "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
	}
	schemas["ModelRouteSpec"] = map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{"tiers": map[string]any{"type": "array", "minItems": 1, "items": ref("WorkspaceRouteTier")}},
		"required":   []string{"tiers"},
	}
	list := paths["/api/v1/workspaces/{slug}/model-routes"].(map[string]any)["get"].(map[string]any)
	list["x-swobu-contract-stage"] = "implemented"
	list["description"] = "Read the current Models routes, including complete tier topology and redacted Connection references. No-store."
	list["responses"].(map[string]any)["200"] = success(map[string]any{"type": "array", "items": ref("WorkspaceRoute")})
	schemas["ModelRouteOrder"] = map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"order": map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1}, "uniqueItems": true}}, "required": []string{"order"}}
	reorder := paths["/api/v1/workspaces/{slug}/model-routes"].(map[string]any)["patch"].(map[string]any)
	reorder["x-swobu-contract-stage"] = "implemented"
	reorder["description"] = "Atomically replace first-match precedence with the exact permutation of configured route patterns. Requires local authority and the Workspace If-Match. No target revisions change."
	reorder["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": ref("ModelRouteOrder")}}}
	reorder["responses"].(map[string]any)["200"] = success(ref("WorkspaceProjection"))
	reorder["responses"].(map[string]any)["412"] = success(ref("WorkspaceServiceError"))
	schemas["ModelRouteMatch"] = map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"matched": map[string]any{"type": "boolean"}, "pattern": map[string]any{"type": "string"},
		"priority": map[string]any{"type": "integer", "minimum": 1}, "tiers": map[string]any{"type": "integer", "minimum": 1}, "targets": map[string]any{"type": "integer", "minimum": 1},
	}, "required": []string{"matched"}}
	preview := paths["/api/v1/workspaces/{slug}/model-match"].(map[string]any)["get"].(map[string]any)
	preview["x-swobu-contract-stage"] = "implemented"
	preview["description"] = "Preview saved first-match model routing through the production Go path.Match resolver, without provider dispatch."
	preview["parameters"] = append(preview["parameters"].([]any), map[string]any{"name": "model", "in": "query", "required": true, "schema": map[string]any{"type": "string", "minLength": 1}})
	preview["responses"].(map[string]any)["200"] = success(ref("ModelRouteMatch"))
	detail := paths["/api/v1/workspaces/{slug}/model-routes/{route}"].(map[string]any)
	read := detail["get"].(map[string]any)
	read["x-swobu-contract-stage"] = "implemented"
	read["description"] = "Read the editable route specification; route identity is the path, target versions are server-owned."
	read["responses"].(map[string]any)["200"] = success(ref("ModelRouteSpec"))
	put := detail["put"].(map[string]any)
	put["x-swobu-contract-stage"] = "implemented"
	put["description"] = "Atomic route create/update with one strong Workspace If-Match. If delivery is uncertain, read the Workspace and reconcile; no replay receipt is issued."
	put["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": ref("ModelRouteSpec")}}}
	put["responses"].(map[string]any)["200"] = success(ref("WorkspaceProjection"))
	put["responses"].(map[string]any)["412"] = success(ref("WorkspaceServiceError"))
	remove := detail["delete"].(map[string]any)
	remove["x-swobu-contract-stage"] = "implemented"
	remove["description"] = "Delete a Models route at the exact Workspace revision; last-route deletion leaves Models enabled but empty."
	remove["responses"].(map[string]any)["200"] = success(ref("WorkspaceProjection"))
}
