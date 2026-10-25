package apischema

// workspaceServiceContracts describes the actual S01 wire response, not the
// broader aspirational Node configuration schema. The mutation result is the
// existing redacted Workspace projection and carries a strong Workspace ETag.
func workspaceServiceContracts(document map[string]any) {
	components := document["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	paths := document["paths"].(map[string]any)
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	str := func() map[string]any { return map[string]any{"type": "string"} }
	obj := func(fields map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": fields, "required": required}
	}
	array := func(elem any) map[string]any { return map[string]any{"type": "array", "items": elem} }
	jsonSuccess := func(description string, schema any) map[string]any {
		return map[string]any{"description": description, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
	}
	enum := map[string]any{"type": "string", "enum": []string{"models", "mcp", "search"}}
	schemas["WorkspaceServiceError"] = obj(map[string]any{
		"code": str(), "message": str(),
	}, "code", "message")
	schemas["ServiceAffectedPrincipal"] = obj(map[string]any{
		"id": str(), "label": str(),
	}, "id", "label")
	schemas["ServiceActivationReview"] = obj(map[string]any{
		"service": enum, "workspace_etag": str(), "access_revision": str(), "publication_revision": str(),
		"affected_principals": array(ref("ServiceAffectedPrincipal")), "published": map[string]any{"type": "boolean"},
		"requires_confirmation": map[string]any{"type": "boolean"}, "warning": str(), "digest": str(),
	}, "service", "workspace_etag", "access_revision", "publication_revision", "affected_principals", "published", "requires_confirmation", "digest")
	schemas["ServiceActivationIntent"] = obj(map[string]any{
		"review_digest": map[string]any{"type": "string", "pattern": "^[a-f0-9]{64}$"},
	})
	schemas["WorkspaceServiceState"] = obj(map[string]any{
		"kind": enum, "enabled": map[string]any{"type": "boolean"}, "configured": map[string]any{"type": "boolean"},
		"activation_review": ref("ServiceActivationReview"),
	}, "kind", "enabled", "configured")
	schemas["WorkspaceRouteTarget"] = obj(map[string]any{
		"id": str(), "model": str(), "protocol": str(), "connection_id": str(),
	}, "id", "model", "protocol", "connection_id")
	schemas["WorkspaceRouteTier"] = obj(map[string]any{
		"targets": array(ref("WorkspaceRouteTarget")),
	}, "targets")
	schemas["WorkspaceRoute"] = obj(map[string]any{
		"name": str(), "tiers": array(ref("WorkspaceRouteTier")),
	}, "name", "tiers")
	schemas["WorkspaceMCPMount"] = obj(map[string]any{
		"alias": str(), "connection_id": str(), "builtin": map[string]any{"type": "string", "enum": []string{"swobu-admin"}},
		"allowed_tools": map[string]any{"type": []string{"array", "null"}, "items": str()},
	}, "alias", "connection_id", "allowed_tools")
	mcpMountSourceSchema(schemas["WorkspaceMCPMount"].(map[string]any))
	schemas["WorkspaceReadiness"] = obj(map[string]any{
		"llm": str(), "mcp": str(), "search": str(),
	}, "llm", "mcp")
	schemas["WorkspaceServiceQuota"] = obj(map[string]any{
		"requests_per_minute": map[string]any{"type": "integer", "minimum": 0, "maximum": 100000},
		"concurrent":          map[string]any{"type": "integer", "minimum": 0, "maximum": 10000},
	}, "requests_per_minute")
	schemas["WorkspaceServiceLimits"] = obj(map[string]any{
		"models": ref("WorkspaceServiceQuota"),
		"mcp":    ref("WorkspaceServiceQuota"),
		"search": ref("WorkspaceServiceQuota"),
	}, "models", "mcp", "search")
	schemas["WorkspaceProjection"] = obj(map[string]any{
		"display_name": str(), "management_etag": str(),
		"management_revision": map[string]any{"type": "integer", "minimum": 1},
		"slug":                str(),
		"routes":              array(ref("WorkspaceRoute")),
		"mcp_mounts":          array(ref("WorkspaceMCPMount")),
		"readiness":           ref("WorkspaceReadiness"),
		"enabled_services":    array(enum),
		"service_limits":      ref("WorkspaceServiceLimits"),
	}, "display_name", "management_etag", "management_revision", "slug", "routes", "readiness", "enabled_services")
	policy := paths["/api/v1/workspaces/{slug}/service-limits"].(map[string]any)
	for _, method := range []string{"get", "put"} {
		op := policy[method].(map[string]any)
		op["x-swobu-contract-stage"] = "implemented"
		op["description"] = "Workspace-wide Models/MCP/Search admission policy. Zero is unlimited. Distinct from /limits, which owns durable spend policy. Local operator authority required."
		op["responses"].(map[string]any)["200"] = jsonSuccess("Current operational limits", ref("WorkspaceServiceLimits"))
		op["responses"].(map[string]any)["200"].(map[string]any)["headers"] = map[string]any{"ETag": map[string]any{"description": "Current Workspace management ETag", "schema": str()}}
		if method == "put" {
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": ref("WorkspaceServiceLimits")}}}
			op["responses"].(map[string]any)["428"] = jsonSuccess("Workspace ETag required", ref("WorkspaceServiceError"))
			op["responses"].(map[string]any)["412"] = jsonSuccess("Workspace ETag stale", ref("WorkspaceServiceError"))
		}
	}
	list := paths["/api/v1/workspaces/{slug}/services"].(map[string]any)["get"].(map[string]any)
	list["x-swobu-contract-stage"] = "implemented"
	list["description"] = "List all released Workspace Services, including disabled and enabled-but-unconfigured states. Snapshot is redacted and no-store."
	list["responses"].(map[string]any)["200"] = jsonSuccess("Three current Workspace Services", array(ref("WorkspaceServiceState")))
	for _, method := range []string{"put", "delete"} {
		operation := paths["/api/v1/workspaces/{slug}/services/{service}"].(map[string]any)[method].(map[string]any)
		operation["x-swobu-contract-stage"] = "implemented"
		operation["description"] = "Checked explicit Service lifecycle. Requires trusted local operator admission and a strong Workspace If-Match; enabling with existing grants or remote publication requires an exact server-issued activation review digest from GET /services. Nonempty Service disable fails with 409."
		if method == "put" {
			operation["requestBody"] = map[string]any{"required": false, "content": map[string]any{"application/json": map[string]any{"schema": ref("ServiceActivationIntent")}}}
			operation["responses"].(map[string]any)["428"] = jsonSuccess("Existing grants or publication require an activation review", ref("WorkspaceServiceError"))
		}
		operation["responses"].(map[string]any)["200"] = jsonSuccess("Updated Workspace", ref("WorkspaceProjection"))
		operation["responses"].(map[string]any)["409"] = jsonSuccess("Configured children block disabling this Service", ref("WorkspaceServiceError"))
		operation["responses"].(map[string]any)["412"] = jsonSuccess("Workspace revision stale", ref("WorkspaceServiceError"))
	}
}
