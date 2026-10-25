package apischema

// authorityContracts documents mounted Principal and Workspace use-permission
// operations; hypothetical grants/Node-admin resources are not public API.
func authorityContracts(document map[string]any) {
	paths := document["paths"].(map[string]any)
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	str := func() map[string]any { return map[string]any{"type": "string"} }
	boolean := func() map[string]any { return map[string]any{"type": "boolean"} }
	number := func() map[string]any { return map[string]any{"type": "integer"} }
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	array := func(item any) map[string]any { return map[string]any{"type": "array", "items": item} }
	object := func(fields map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": fields, "required": required}
	}
	nullable := func(name string) map[string]any {
		return map[string]any{"oneOf": []any{ref(name), map[string]any{"type": "null"}}}
	}
	success := func(schema any) map[string]any {
		return map[string]any{"description": "Admitted local Operator state; no-store", "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
	}
	query := func() map[string]any {
		return map[string]any{"name": "dry_run", "in": "query", "required": false, "schema": boolean(), "description": "true previews the exact impact without committing or reserving a retry key"}
	}
	optionalHeader := func(name string) map[string]any {
		return map[string]any{"name": name, "in": "header", "required": false, "schema": str()}
	}
	pathItem := func(path, method string) map[string]any { return paths[path].(map[string]any)[method].(map[string]any) }
	promote := func(path, method, description string, response any, code string) map[string]any {
		op := pathItem(path, method)
		op["x-swobu-contract-stage"] = "implemented"
		op["description"] = description
		op["responses"].(map[string]any)[code] = success(response)
		return op
	}
	body := func(op map[string]any, schema any) {
		op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
	}
	schemas["ServiceRequestLimit"] = object(map[string]any{"service": map[string]any{"type": "string", "enum": []string{"models", "mcp", "search"}}, "requests_per_minute": map[string]any{"type": "integer", "minimum": 1, "maximum": 100000}}, "service", "requests_per_minute")
	schemas["PrincipalPermission"] = object(map[string]any{"workspace": str(), "action": map[string]any{"type": "string", "enum": []string{"use"}}, "expires_at": map[string]any{"type": "string", "format": "date-time"}, "services": array(map[string]any{"type": "string", "enum": []string{"models", "mcp", "search"}}), "request_limits": array(ref("ServiceRequestLimit"))}, "workspace", "action")
	schemas["PrincipalCredential"] = object(map[string]any{"id": str(), "workspace": str(), "created_at": map[string]any{"type": "string", "format": "date-time"}}, "id", "workspace", "created_at")
	schemas["PrincipalAuthority"] = object(map[string]any{"id": str(), "label": str(), "kind": str(), "status": str(), "revision": str(), "created_at": map[string]any{"type": "string", "format": "date-time"}, "permissions": array(ref("PrincipalPermission"))}, "id", "label", "kind", "status", "revision", "permissions")
	schemas["PrincipalLabel"] = object(map[string]any{"label": str()}, "label")
	schemas["CredentialIssueRequest"] = object(map[string]any{"workspace": str()}, "workspace")
	schemas["AccessIdentity"] = object(map[string]any{"id": str(), "name": str(), "kind": str(), "status": str(), "created_at": str(), "credentials": array(ref("PrincipalCredential"))}, "id", "name", "kind", "status", "credentials")
	schemas["CredentialIssue"] = object(map[string]any{"credential": ref("PrincipalCredential"), "access": ref("AccessIdentity"), "token": str(), "secret_not_recoverable": boolean()}, "credential", "access")
	schemas["PermissionDraft"] = object(map[string]any{"action": map[string]any{"type": "string", "enum": []string{"use"}}, "expires_at": map[string]any{"type": "string", "format": "date-time"}, "services": map[string]any{"type": "array", "minItems": 1, "uniqueItems": true, "items": map[string]any{"type": "string", "enum": []string{"models", "mcp", "search"}}}, "request_limits": array(ref("ServiceRequestLimit"))})
	schemas["PermissionReview"] = object(map[string]any{"principal": str(), "workspace": str(), "operation": str(), "revision": str(), "before": nullable("PrincipalPermission"), "after": nullable("PrincipalPermission"), "affected_credentials": number(), "approval_class": str(), "warning": str()}, "principal", "workspace", "operation", "revision", "before", "after", "affected_credentials", "approval_class", "warning")
	schemas["PermissionState"] = object(map[string]any{"principal": str(), "workspace": str(), "revision": str(), "permission": nullable("PrincipalPermission"), "allowed": boolean(), "replayed": boolean()}, "principal", "workspace", "revision", "permission", "allowed")
	schemas["CredentialReview"] = object(map[string]any{"principal": str(), "credential": ref("PrincipalCredential"), "revision": str(), "approval_class": str(), "warning": str()}, "principal", "credential", "revision", "approval_class", "warning")
	schemas["CredentialRevoked"] = object(map[string]any{"principal": ref("PrincipalAuthority"), "credential": str(), "revoked": boolean(), "replayed": boolean()}, "principal", "credential", "revoked", "replayed")
	promote("/api/v1/principals", "get", "List managed Principals with their Workspace use relationships; never returns credentials or bearer secrets.", array(ref("PrincipalAuthority")), "200")
	principalCreate := promote("/api/v1/principals", "post", "Create Principal metadata only. Original idempotency key must survive uncertain retries.", ref("PrincipalAuthority"), "201")
	body(principalCreate, ref("PrincipalLabel"))
	principalCreate["responses"].(map[string]any)["200"] = success(ref("PrincipalAuthority"))
	promote("/api/v1/principals/{id}", "get", "Read exact Principal and grants, redacted and revisioned.", ref("PrincipalAuthority"), "200")
	principalUpdate := promote("/api/v1/principals/{id}", "patch", "Rename Principal label at exact strong ETag; never changes its authority.", ref("PrincipalAuthority"), "200")
	body(principalUpdate, ref("PrincipalLabel"))
	promote("/api/v1/principals/{id}/credentials", "get", "List credential metadata. Secret tokens are never returned.", array(ref("PrincipalCredential")), "200")
	issue := promote("/api/v1/principals/{id}/credentials", "post", "Issue Workspace bearer once. Exact retry returns metadata and secret_not_recoverable; never a second token.", ref("CredentialIssue"), "201")
	body(issue, ref("CredentialIssueRequest"))
	issue["responses"].(map[string]any)["200"] = success(ref("CredentialIssue"))
	revokeCredential := promote("/api/v1/principals/{id}/credentials/{credential}", "delete", "dry_run=true previews selected credential impact; commit requires preview ETag, original idempotency key and trusted admission.", map[string]any{"oneOf": []any{ref("CredentialReview"), ref("CredentialRevoked")}}, "200")
	revokeCredential["parameters"] = []any{
		map[string]any{"name": "id", "in": "path", "required": true, "schema": str()},
		map[string]any{"name": "credential", "in": "path", "required": true, "schema": str()},
		optionalHeader("If-Match"), optionalHeader("Idempotency-Key"), query(),
	}
	body(revokeCredential, object(map[string]any{}))
	promote("/api/v1/workspaces/{slug}/permissions/{id}", "get", "Read one natural Workspace use relationship at current authority revision.", ref("PermissionState"), "200")
	for _, method := range []string{"put", "delete"} {
		op := promote("/api/v1/workspaces/{slug}/permissions/{id}", method, "Review or commit a Workspace permission with scoped Services and optional per-Service request ceilings. Omission of request_limits preserves existing ceilings; [] clears them. Omitting services preserves an existing scoped grant; a new omitted-service grant permits all enabled Services; the Web Operator sends an explicit selection. Review never grants or issues credentials; apply binds the preview revision, Services, and retry identity.", map[string]any{"oneOf": []any{ref("PermissionReview"), ref("PermissionState")}}, "200")
		op["parameters"] = []any{
			map[string]any{"name": "slug", "in": "path", "required": true, "schema": str()},
			map[string]any{"name": "id", "in": "path", "required": true, "schema": str()},
			optionalHeader("If-Match"), optionalHeader("Idempotency-Key"), query(),
		}
		if method == "put" {
			body(op, ref("PermissionDraft"))
		} else {
			body(op, object(map[string]any{}))
		}
	}
	schemas["WorkspacePublication"] = object(map[string]any{"workspace": str(), "published": boolean(), "address": str(), "url": str(), "revision": str()}, "workspace", "published", "address", "revision")
	schemas["RemoteAddress"] = object(map[string]any{"id": str(), "url": str(), "managed": boolean()}, "id", "managed")
	promote("/api/v1/remote-addresses", "get", "Built-in relay address; URL exists only once its transport identity has been created.", array(ref("RemoteAddress")), "200")
	promote("/api/v1/workspaces/{slug}/publications", "get", "Reachability through the built-in relay; Workspace credentials and Service policy still apply.", ref("WorkspacePublication"), "200")
	for _, method := range []string{"put", "delete"} {
		op := promote("/api/v1/workspaces/{slug}/publications/{remote_address_id}", method, "Enable or disable relay reachability at the current publication revision. Workspace credential policy remains independent.", ref("WorkspacePublication"), "200")
		op["parameters"] = []any{map[string]any{"name": "slug", "in": "path", "required": true, "schema": str()}, map[string]any{"name": "remote_address_id", "in": "path", "required": true, "schema": map[string]any{"type": "string", "enum": []string{"relay"}}}, optionalHeader("If-Match")}
	}
	schemas["SpendPolicy"] = object(map[string]any{"currency": map[string]any{"type": "string", "enum": []string{"USD"}}, "amount_cents": number(), "window": map[string]any{"type": "string", "enum": []string{"day", "month"}}, "mode": map[string]any{"type": "string", "enum": []string{"stop_new_billable_work", "observe_only"}}, "unknown_price": map[string]any{"type": "string", "enum": []string{"reject"}}}, "currency", "amount_cents", "window", "mode", "unknown_price")
	schemas["SpendStatus"] = object(map[string]any{"workspace": str(), "limit": nullable("SpendPolicy"), "window_key": str(), "window_start": str(), "window_end": str(), "currency": str(), "reserved_cents": str(), "settled_cents": str(), "uncertain_cents": str(), "charged_cents": str(), "remaining_cents": map[string]any{"type": []string{"string", "null"}}, "unknown_price_operations": number(), "partial_quantity_operations": number(), "unquoted_operations": number()}, "workspace", "limit", "window_key", "window_start", "window_end", "currency", "reserved_cents", "settled_cents", "uncertain_cents", "charged_cents", "remaining_cents")
	schemas["SpendApproval"] = object(map[string]any{"id": str(), "workspace": str(), "operation": str(), "digest": str(), "revision": str(), "expires_at": str(), "used": boolean()}, "id", "workspace", "operation", "digest", "revision", "expires_at", "used")
	promote("/api/v1/workspaces/{slug}/limits", "get", "Read configured spending policy, or 404 when not configured.", ref("SpendPolicy"), "200")
	promote("/api/v1/workspaces/{slug}/limits/status", "get", "Return retained reservation and settlement evidence bound to the strong accounting ETag. No budget is represented as null.", ref("SpendStatus"), "200")
	previewLimit := promote("/api/v1/workspaces/{slug}/limits/preview", "post", "Preview a spending policy via a short-lived approval bound to the exact accounting revision and candidate.", ref("SpendApproval"), "200")
	previewLimit["parameters"] = []any{map[string]any{"name": "slug", "in": "path", "required": true, "schema": str()}}
	body(previewLimit, ref("SpendPolicy"))
	applyLimit := promote("/api/v1/workspaces/{slug}/limits/apply", "post", "Consume an exact unexpired reviewed approval before committing the policy at the reviewed revision.", ref("SpendPolicy"), "200")
	applyLimit["parameters"] = []any{map[string]any{"name": "slug", "in": "path", "required": true, "schema": str()}}
	body(applyLimit, object(map[string]any{"approval_id": str(), "limit": ref("SpendPolicy")}, "approval_id", "limit"))
	removeLimit := promote("/api/v1/workspaces/{slug}/limits", "delete", "Clear only the spending policy at exact accounting ETag. Retained reservations and settlements survive.", map[string]any{"type": "null"}, "204")
	_ = removeLimit
}
