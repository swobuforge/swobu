package apischema

// workspaceContracts describes the existing, redacted Workspace command/read
// projections. Slug is immutable; ETags are server-owned and every authored
// change is checked against the current Workspace generation.
func workspaceContracts(document map[string]any) {
	components := document["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	paths := document["paths"].(map[string]any)
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	text := func() map[string]any { return map[string]any{"type": "string"} }
	array := func(item any) map[string]any { return map[string]any{"type": "array", "items": item} }
	object := func(properties map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
	}
	response := func(description, name string) map[string]any {
		return map[string]any{"description": description, "content": map[string]any{"application/json": map[string]any{"schema": ref(name)}}}
	}
	revisionHeader := map[string]any{"ETag": map[string]any{"description": "Strong quoted Workspace management revision", "required": true, "schema": text()}}
	schemas["WorkspaceSummary"] = object(map[string]any{"slug": text(), "display_name": text(), "route_count": map[string]any{"type": "integer", "minimum": 0}}, "slug", "display_name", "route_count")
	schemas["WorkspaceCreateDraft"] = object(map[string]any{
		"slug": text(), "display_name": text(), "initial_route": text(), "target": ref("WorkspaceRouteTarget"),
	}, "slug")
	schemas["WorkspaceDisplayNameDraft"] = object(map[string]any{"display_name": text()}, "display_name")
	for _, entry := range []struct {
		path, method, stage, desc, result string
		request, status                   string
	}{
		{"/api/v1/workspaces", "get", "implemented", "Current Workspace summaries with no private credentials.", "WorkspaceSummary", "", "200"},
		{"/api/v1/workspaces", "post", "implemented", "Create a Workspace at an immutable slug, with one durable idempotency key per intent.", "WorkspaceProjection", "WorkspaceCreateDraft", "201"},
		{"/api/v1/workspaces/{slug}", "get", "implemented", "Read one Workspace with current management ETag and service configuration.", "WorkspaceProjection", "", "200"},
		{"/api/v1/workspaces/{slug}", "patch", "implemented", "Update presentation metadata at the reviewed Workspace revision; slug is immutable.", "WorkspaceProjection", "WorkspaceDisplayNameDraft", "200"},
	} {
		operation := paths[entry.path].(map[string]any)[entry.method].(map[string]any)
		operation["x-swobu-contract-stage"] = entry.stage
		operation["description"] = entry.desc + " Requires local control authority; all responses are no-store."
		var success map[string]any
		if entry.path == "/api/v1/workspaces" && entry.method == "get" {
			success = map[string]any{"description": "Current Workspace summaries", "content": map[string]any{"application/json": map[string]any{"schema": array(ref(entry.result))}}}
		} else {
			success = response("Committed Workspace projection", entry.result)
			success["headers"] = revisionHeader
		}
		operation["responses"].(map[string]any)[entry.status] = success
		if entry.method == "post" {
			// Idempotent replay uses 200, never pretends to be a new creation.
			operation["responses"].(map[string]any)["200"] = success
		}
		if entry.request != "" {
			operation["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": ref(entry.request)}}}
		}
	}
}
