package apischema

// serviceAuthoringContracts describes only implemented MCP review/commit,
// Connection-backed Search attachment and mount deletion. No verification
// endpoint is promoted until it exists in the daemon.
func serviceAuthoringContracts(document map[string]any) {
	paths := document["paths"].(map[string]any)
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	str := func() map[string]any { return map[string]any{"type": "string"} }
	arr := func(item any) map[string]any { return map[string]any{"type": "array", "items": item} }
	obj := func(fields map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": fields, "required": required}
	}
	response := func(label string, schema any) map[string]any {
		return map[string]any{"description": label, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
	}
	schemas["MCPMount"] = obj(map[string]any{"alias": str(), "connection_id": str(), "builtin": map[string]any{"type": "string", "enum": []string{"swobu-admin"}}, "selected_tools": arr(str())}, "alias", "connection_id", "selected_tools")
	schemas["MCPMountReview"] = obj(map[string]any{"workspace_etag": str(), "digest": str(), "connection_revision": str(), "access_revision": str(), "publication_revision": str()}, "workspace_etag", "digest", "connection_revision", "access_revision", "publication_revision")
	schemas["MCPMountDraft"] = obj(map[string]any{"alias": str(), "connection_id": str(), "builtin": map[string]any{"type": "string", "enum": []string{"swobu-admin"}}, "selected_tools": arr(str()), "review": ref("MCPMountReview")}, "selected_tools")
	schemas["MCPMountPrincipal"] = obj(map[string]any{"id": str(), "label": str()}, "id", "label")
	schemas["MCPMountPreview"] = obj(map[string]any{
		"workspace": str(), "alias": str(), "connection_id": str(), "builtin": map[string]any{"type": "string", "enum": []string{"swobu-admin"}}, "endpoint": str(), "execution_identity": str(), "identity_verified": map[string]any{"type": "boolean"}, "permission_summary": str(),
		"allowed_tools": arr(str()), "potentially_effectful_tools": arr(str()), "existing_mount": ref("WorkspaceMCPMount"), "affected_principals": arr(ref("MCPMountPrincipal")),
		"published": map[string]any{"type": "boolean"}, "revision": str(), "access_revision": str(), "publication_revision": str(), "digest": str(), "connection_revision": str(), "approval_class": str(), "warning": str(),
	}, "workspace", "alias", "connection_id", "endpoint", "execution_identity", "identity_verified", "permission_summary", "allowed_tools", "potentially_effectful_tools", "affected_principals", "published", "revision", "access_revision", "publication_revision", "digest", "connection_revision", "approval_class", "warning")
	for _, name := range []string{"MCPMount", "MCPMountDraft", "MCPMountPreview"} {
		mcpMountSourceSchema(schemas[name].(map[string]any))
	}
	list := paths["/api/v1/workspaces/{slug}/mcp-mounts"].(map[string]any)["get"].(map[string]any)
	list["x-swobu-contract-stage"] = "implemented"
	list["description"] = "Current redacted MCP mounts belonging to this Workspace."
	list["responses"].(map[string]any)["200"] = response("Mounts", arr(ref("MCPMount")))
	detail := paths["/api/v1/workspaces/{slug}/mcp-mounts/{alias}"].(map[string]any)
	get := detail["get"].(map[string]any)
	get["x-swobu-contract-stage"] = "implemented"
	get["description"] = "Read one current mount."
	get["responses"].(map[string]any)["200"] = response("Mount", ref("MCPMount"))
	put := detail["put"].(map[string]any)
	put["x-swobu-contract-stage"] = "implemented"
	put["description"] = "dry_run=true produces an advisory review, dry_run=false applies an exact review with strong Workspace ETag and unchanged Connection revision."
	// Preview is read-only. If-Match is mandatory only when applying the review;
	// express the conditional requirement in the description, not by falsely
	// requiring the header on preview requests.
	parameters := put["parameters"].([]any)
	for i, parameter := range parameters {
		if parameter.(map[string]any)["$ref"] == "#/components/parameters/IfMatch" {
			parameters[i] = map[string]any{"in": "header", "name": "If-Match", "required": false, "schema": map[string]any{"type": "string"}, "description": "Required when dry_run=false; must equal the reviewed Workspace ETag."}
		}
	}
	put["parameters"] = append(parameters, map[string]any{"in": "query", "name": "dry_run", "required": true, "schema": map[string]any{"type": "boolean"}})
	put["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": ref("MCPMountDraft")}}}
	put["responses"].(map[string]any)["200"] = response("Review or committed mount", map[string]any{"oneOf": []any{ref("MCPMountPreview"), ref("WorkspaceProjection")}})
	remove := detail["delete"].(map[string]any)
	remove["x-swobu-contract-stage"] = "implemented"
	remove["description"] = "Delete a current MCP mount at a strong Workspace ETag. Other mounts and upstream Connections are retained."
	remove["responses"].(map[string]any)["200"] = response("Workspace", ref("WorkspaceProjection"))
	schemas["SearchRoute"] = obj(map[string]any{"connection_id": str(), "fallback_connection_ids": map[string]any{"type": "array", "items": str(), "maxItems": 7}}, "connection_id", "fallback_connection_ids")
	search := paths["/api/v1/workspaces/{slug}/search-route"].(map[string]any)
	for _, method := range []string{"get", "put", "delete"} {
		op := search[method].(map[string]any)
		op["x-swobu-contract-stage"] = "implemented"
		op["description"] = "Ordered primary and fallback Search Connections, independent of provider-native LLM search. An upstream failure advances to the next configured Connection; an empty successful result is terminal."
		if method == "get" {
			op["responses"].(map[string]any)["200"] = response("Configured Search route", ref("SearchRoute"))
		} else {
			op["responses"].(map[string]any)["200"] = response("Workspace", ref("WorkspaceProjection"))
			if method == "put" {
				op["requestBody"] = map[string]any{
					"required": true,
					"content": map[string]any{
						"application/json": map[string]any{"schema": ref("SearchRoute")},
					},
				}
			}
		}
	}
}

// The source discriminator is closed: external identity cannot simultaneously
// request builtin authority. Empty connection_id is tolerated only for the
// builtin wire variant because redacted projections retain that field.
func mcpMountSourceSchema(schema map[string]any) {
	schema["oneOf"] = []any{
		map[string]any{"required": []string{"connection_id"}, "properties": map[string]any{"connection_id": map[string]any{"type": "string", "minLength": 1}}, "not": map[string]any{"required": []string{"builtin"}}},
		map[string]any{"required": []string{"builtin"}, "properties": map[string]any{"connection_id": map[string]any{"type": "string", "enum": []string{""}}, "builtin": map[string]any{"type": "string", "enum": []string{"swobu-admin"}}}},
	}
}
