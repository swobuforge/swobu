package apischema

// transportContracts closes mounted operations whose admission and response
// owners live outside the resource handlers: browser bootstrap, Activity SSE,
// and high-risk deletion. These are implemented entrypoints, not future APIs.
func transportContracts(document map[string]any) {
	paths := document["paths"].(map[string]any)
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	str := func() map[string]any { return map[string]any{"type": "string"} }
	boolean := func() map[string]any { return map[string]any{"type": "boolean"} }
	integer := func() map[string]any { return map[string]any{"type": "integer"} }
	object := func(fields map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": fields, "required": required}
	}
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	response := func(description string, schema any) map[string]any {
		return map[string]any{"description": description, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
	}
	parameter := func(name, location string, schema any, required bool) map[string]any {
		return map[string]any{"name": name, "in": location, "schema": schema, "required": required}
	}
	operation := func(path, method string) map[string]any {
		op := paths[path].(map[string]any)[method].(map[string]any)
		op["x-swobu-contract-stage"] = "implemented"
		return op
	}
	schemas["BrowserLaunch"] = object(map[string]any{"launch_path": str()}, "launch_path")
	launch := operation("/api/v1/browser-launches", "post")
	launch["description"] = "Native local host only. Creates one expiring one-shot browser launch path; the URL fragment is not a reusable Workspace credential."
	launch["responses"].(map[string]any)["201"] = response("Created one-shot native browser launch", ref("BrowserLaunch"))

	schemas["BrowserSessionExchange"] = object(map[string]any{"token": str()}, "token")
	session := operation("/api/v1/browser-session", "post")
	session["description"] = "Same-origin browser exchange of the one-shot launch token for a local, HttpOnly session cookie. One use only; no token returned."
	session["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": ref("BrowserSessionExchange")}}}
	session["responses"].(map[string]any)["204"] = map[string]any{"description": "Local browser session cookie issued (no response body)"}

	stream := operation("/api/v1/activity/stream", "get")
	stream["description"] = "Bounded content-free live Activity observations (not durable replay). Reconnect starts a fresh view; Last-Event-ID and retained cursors are rejected."
	stream["parameters"] = []any{
		parameter("workspace", "query", str(), false),
		parameter("kind", "query", map[string]any{"type": "string", "enum": []string{"exchange", "mcp_operation"}}, false),
	}
	stream["responses"].(map[string]any)["200"] = map[string]any{"description": "Server-sent events, each containing a bounded Activity observation", "content": map[string]any{"text/event-stream": map[string]any{"schema": str()}}}

	schemas["PrincipalRevocation"] = object(map[string]any{"principal": ref("PrincipalAuthority"), "credential_count": integer(), "replayed": boolean()}, "principal", "credential_count", "replayed")
	principal := operation("/api/v1/principals/{id}", "delete")
	principal["description"] = "Preview or revoke one Principal and associated credentials at an exact Principal revision. Preview is not permission to commit; apply requires the original idempotency key and trusted host admission."
	principal["parameters"] = []any{parameter("id", "path", str(), true), parameter("If-Match", "header", str(), true), parameter("Idempotency-Key", "header", str(), false), parameter("dry_run", "query", boolean(), false)}
	principal["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": object(map[string]any{})}}}
	principal["responses"].(map[string]any)["200"] = response("Reviewed Principal revocation or committed, revisioned result", ref("PrincipalRevocation"))

	schemas["WorkspaceDeletionPreview"] = object(map[string]any{
		"workspace": str(), "management_etag": str(), "management_revision": integer(),
		"route_count": integer(), "approval_class": str(), "diff": str(), "warning": str(),
	}, "workspace", "management_etag", "management_revision", "route_count", "approval_class", "diff", "warning")
	schemas["WorkspaceDeletionReceipt"] = object(map[string]any{"deleted": boolean()}, "deleted")
	workspace := operation("/api/v1/workspaces/{slug}", "delete")
	workspace["description"] = "Review the exact Workspace deletion impact with dry_run=true. Commit requires the Workspace ETag, original idempotency key, and local operator authority; Access and publication constraints are checked by the application owner."
	workspace["parameters"] = []any{parameter("slug", "path", str(), true), parameter("If-Match", "header", str(), false), parameter("Idempotency-Key", "header", str(), false), parameter("dry_run", "query", boolean(), false)}
	workspace["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": object(map[string]any{})}}}
	workspace["responses"].(map[string]any)["200"] = response("Deletion preview or committed deletion acknowledgement", map[string]any{"oneOf": []any{ref("WorkspaceDeletionPreview"), ref("WorkspaceDeletionReceipt")}})
}
