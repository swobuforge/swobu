package apischema

// connectionContracts describes the redacted response sum, independently of the
// private persistence codec. Request authoring must never reuse these views.
func connectionContracts(document map[string]any) {
	components := document["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	text := func() map[string]any { return map[string]any{"type": "string"} }
	enum := func(values ...string) map[string]any { return map[string]any{"type": "string", "enum": values} }
	object := func(properties map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": properties, "required": required}
	}
	revision := map[string]any{"type": "string", "pattern": "^[1-9][0-9]*$"}
	id := map[string]any{"type": "string", "pattern": "^[a-z0-9_-]+$"}
	timestamp := map[string]any{"type": "string", "format": "date-time"}
	boolean := map[string]any{"type": "boolean"}
	kind := enum("model", "mcp", "search")
	schemas["ConnectionReference"] = object(map[string]any{
		"workspace": text(), "kind": kind, "route": text(), "target": text(), "mount": text(),
	}, "workspace", "kind")
	schemas["ConnectionReferences"] = object(map[string]any{
		"connection_id": id, "connection_revision": revision,
		"consumers": map[string]any{"type": "array", "items": ref("ConnectionReference")},
	}, "connection_id", "connection_revision", "consumers")
	schemas["ConnectionCheck"] = object(map[string]any{
		"connection_id": id, "connection_revision": revision, "checked_at": timestamp,
		"reachability":       enum("unknown", "reachable", "unreachable"),
		"authentication":     enum("unknown", "accepted", "rejected", "not_required"),
		"capability_summary": map[string]any{"type": "array", "uniqueItems": true, "items": enum("models", "tools", "search")},
		"sanitized_error":    enum("timeout", "network_denied", "unavailable", "authentication_failed", "invalid_response", "credential_unavailable"),
	}, "connection_id", "connection_revision", "checked_at", "reachability", "authentication", "capability_summary")
	modelShapes := []struct {
		name, shape      string
		fields           []string
		optionalEndpoint bool
	}{
		{"StandardModel", "standard", []string{"endpoint"}, true},
		{"ZAIModel", "zai", []string{"access"}, false},
		{"BedrockModel", "bedrock", []string{"region", "endpoint"}, false},
		{"CustomModel", "custom", []string{"endpoint"}, false},
	}
	modelVariants := []any{}
	for _, shape := range modelShapes {
		properties := map[string]any{"shape": map[string]any{"type": "string", "const": shape.shape}, "provider_id": text(), "authentication_configured": boolean}
		required := []string{"shape", "provider_id", "authentication_configured"}
		for _, field := range shape.fields {
			properties[field] = text()
			if field != "endpoint" || !shape.optionalEndpoint {
				required = append(required, field)
			}
		}
		schemas["Connection"+shape.name] = object(properties, required...)
		modelVariants = append(modelVariants, ref("Connection"+shape.name))
	}
	schemas["ConnectionMCPHTTP"] = object(map[string]any{
		"transport": map[string]any{"type": "string", "const": "streamable_http"}, "endpoint": text(), "authentication_configured": boolean,
	}, "transport", "endpoint", "authentication_configured")
	schemas["ConnectionMCPStdio"] = object(map[string]any{
		"transport": map[string]any{"type": "string", "const": "stdio"}, "executable_name": text(),
		"argument_count": map[string]any{"type": "integer", "minimum": 0}, "environment_binding_count": map[string]any{"type": "integer", "minimum": 0},
	}, "transport", "executable_name", "argument_count", "environment_binding_count")
	for _, name := range []string{"ConnectionMCPHTTP", "ConnectionMCPStdio"} {
		schema := schemas[name].(map[string]any)
		properties := schema["properties"].(map[string]any)
		properties["execution_identity"] = text()
		properties["permission_summary"] = text()
		properties["identity_verified"] = map[string]any{"type": "boolean", "const": false}
		schema["required"] = append(schema["required"].([]string), "execution_identity", "permission_summary", "identity_verified")
	}
	schemas["ConnectionSearch"] = object(map[string]any{
		"provider_id": text(), "endpoint": text(), "authentication_configured": boolean,
	}, "provider_id", "endpoint", "authentication_configured")
	// Couple the kind and spec sum so generated clients can narrow by kind.
	variants := []any{}
	for _, entry := range []struct {
		kind string
		spec map[string]any
	}{
		{"model", map[string]any{"oneOf": modelVariants}},
		{"mcp", map[string]any{"oneOf": []any{ref("ConnectionMCPHTTP"), ref("ConnectionMCPStdio")}}},
		{"search", ref("ConnectionSearch")},
	} {
		variants = append(variants, object(map[string]any{
			"id": id, "display_name": text(), "kind": map[string]any{"type": "string", "const": entry.kind},
			"spec": entry.spec, "revision": revision, "created_at": timestamp, "updated_at": timestamp, "latest_check": ref("ConnectionCheck"),
		}, "id", "display_name", "kind", "spec", "revision", "created_at", "updated_at"))
	}
	schemas["Connection"] = map[string]any{"oneOf": variants}
	schemas["ConnectionMCPCatalogTool"] = object(map[string]any{
		"name": text(), "remote_name": text(), "connection_id": id, "connection_revision": revision,
	}, "name", "remote_name", "connection_id", "connection_revision")
	schemas["ConnectionMCPCatalog"] = map[string]any{"type": "array", "items": ref("ConnectionMCPCatalogTool")}
	schemas["ConnectionMutationProblem"] = map[string]any{"allOf": []any{
		ref("ControlProblem"), map[string]any{"type": "object", "properties": map[string]any{
			"consumers": map[string]any{"type": "array", "items": ref("ConnectionReference")}, "current": ref("Connection"),
		}},
	}}
	paths := document["paths"].(map[string]any)
	response := func(description string, schema map[string]any, media string) map[string]any {
		return map[string]any{"description": description, "content": map[string]any{media: map[string]any{"schema": schema}}}
	}
	check := paths["/api/v1/connections/{id}/checks"].(map[string]any)["post"].(map[string]any)
	check["x-swobu-contract-stage"] = "implemented"
	check["description"] = "Check the stored Connection at the reviewed strong If-Match revision. No request body. Observation is ephemeral, no-store and revision-bound; probes do not mutate configuration, credentials or Workspace catalogs. Live operator authority is rechecked before observation publication."
	check["parameters"] = []any{map[string]any{"in": "path", "name": "id", "required": true, "schema": id}, map[string]any{"$ref": "#/components/parameters/IfMatch"}}
	delete(check, "requestBody")
	check["responses"] = map[string]any{
		"200":     response("Sanitized observation for the reviewed revision", ref("ConnectionCheck"), "application/json"),
		"412":     response("Connection changed before or during the probe", ref("ConnectionMutationProblem"), "application/problem+json"),
		"default": response("Canonical control failure", ref("ControlProblem"), "application/problem+json"),
	}
	for _, read := range []struct {
		path   string
		schema map[string]any
		etag   bool
	}{
		{"/api/v1/connections", map[string]any{"type": "array", "items": ref("Connection")}, false},
		{"/api/v1/connections/{id}", ref("Connection"), true},
		{"/api/v1/connections/{id}/references", ref("ConnectionReferences"), false},
	} {
		operation := paths[read.path].(map[string]any)["get"].(map[string]any)
		operation["x-swobu-contract-stage"] = "implemented"
		operation["description"] = "Redacted Connection read, admitted local control only. Responses are no-store."
		success := response("Current redacted Connection view", read.schema, "application/json")
		if read.etag {
			success["headers"] = map[string]any{"ETag": map[string]any{"required": true, "schema": text(), "description": "Strong quoted resource revision"}}
		}
		operation["responses"].(map[string]any)["200"] = success
		if read.path == "/api/v1/connections" {
			operation["parameters"] = []any{
				map[string]any{"in": "query", "name": "kind", "schema": kind},
				map[string]any{"in": "query", "name": "q", "schema": text()},
			}
		}
	}
	catalog := paths["/api/v1/connections/{id}/mcp-catalog"].(map[string]any)["get"].(map[string]any)
	catalog["x-swobu-contract-stage"] = "implemented"
	catalog["description"] = "Secret-free MCP declarations from the current revision-bound Connection runtime binding."
	catalog["parameters"] = []any{map[string]any{"in": "path", "name": "id", "required": true, "schema": id}}
	catalog["responses"] = map[string]any{
		"200":     response("Current MCP catalog for the Connection", ref("ConnectionMCPCatalog"), "application/json"),
		"default": response("Canonical control failure", ref("ControlProblem"), "application/problem+json"),
	}
	deletion := paths["/api/v1/connections/{id}"].(map[string]any)["delete"].(map[string]any)
	deletion["x-swobu-contract-stage"] = "implemented"
	deletion["description"] = "Delete an unused Connection at the reviewed revision, without cascade. Success returns 204; a repeat after deletion returns 404 without another management fact. Browser session and same-origin mutation admission are required."
	deletion["responses"] = map[string]any{
		"204":     map[string]any{"description": "Connection deleted; no response body"},
		"409":     response("Exact consumers prevent deletion", ref("ConnectionMutationProblem"), "application/problem+json"),
		"412":     response("Revision changed; current is an advisory redacted reload summary", ref("ConnectionMutationProblem"), "application/problem+json"),
		"default": response("Canonical control failure", ref("ControlProblem"), "application/problem+json"),
	}
	schemas["ConnectionCredentialReplacement"] = object(map[string]any{
		"secret":      map[string]any{"type": "string", "minLength": 1, "writeOnly": true},
		"environment": text(), "same_identity": map[string]any{"type": "boolean", "const": true},
	}, "secret", "same_identity")
	schemas["ConnectionCredentialReplacementResult"] = object(map[string]any{"connection": ref("Connection"), "replayed": boolean}, "connection", "replayed")
	replacement := paths["/api/v1/connections/{id}/credential-replacements"].(map[string]any)["post"].(map[string]any)
	replacement["x-swobu-contract-stage"] = "implemented"
	replacement["description"] = "Replace one existing credential binding for the same external identity. Retry with the original strong If-Match, Idempotency-Key and material; durable staging reuses material and returns the original result. Responses are redacted and no-store."
	replacement["parameters"] = append(replacement["parameters"].([]any), map[string]any{"$ref": "#/components/parameters/IfMatch"})
	replacement["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": ref("ConnectionCredentialReplacement")}}}
	replacement["responses"] = map[string]any{
		"200":     response("Original committed redacted result", ref("ConnectionCredentialReplacementResult"), "application/json"),
		"412":     response("Reviewed revision changed", ref("ConnectionMutationProblem"), "application/problem+json"),
		"default": response("Canonical control failure", ref("ControlProblem"), "application/problem+json"),
	}

	// Authoring is a separate closed sum: private credential refs and stdio intent
	// are write-only, never reconstructed from the redacted response projection.
	credential := map[string]any{"type": "string", "writeOnly": true}
	configShapes := []struct {
		name, shape string
		fields      map[string]any
		required    []string
	}{
		{"Standard", "standard", map[string]any{"endpoint": text(), "credential_ref": credential}, []string{"credential_ref"}},
		{"ZAI", "zai", map[string]any{"access": enum("general_api", "coding_plan"), "credential_ref": credential}, []string{"access", "credential_ref"}},
		{"Bedrock", "bedrock", map[string]any{"region": text(), "endpoint": text(), "credential_ref": credential}, []string{"region", "endpoint"}},
		{"Custom", "custom", map[string]any{"endpoint": text(), "header": text(), "credential_ref": credential}, []string{"endpoint"}},
	}
	modelDrafts := []any{}
	for _, shape := range configShapes {
		name := "Connection" + shape.name + "DraftSpec"
		schemas[name] = object(map[string]any{"shape": enum(shape.shape), "provider_id": text(), "configuration": object(shape.fields, shape.required...)}, "shape", "provider_id", "configuration")
		modelDrafts = append(modelDrafts, ref(name))
	}
	schemas["ConnectionMCPHTTPDraftSpec"] = object(map[string]any{"transport": enum("streamable_http"), "configuration": object(map[string]any{"endpoint": text(), "credential_ref": credential}, "endpoint")}, "transport", "configuration")
	schemas["ConnectionMCPStdioDraftSpec"] = object(map[string]any{"transport": enum("stdio"), "configuration": object(map[string]any{
		"executable": map[string]any{"type": "string", "writeOnly": true}, "args": map[string]any{"type": "array", "items": text(), "writeOnly": true},
		"env_refs": map[string]any{"type": "object", "additionalProperties": credential, "writeOnly": true},
	}, "executable")}, "transport", "configuration")
	for _, name := range []string{"ConnectionMCPHTTPDraftSpec", "ConnectionMCPStdioDraftSpec"} {
		properties := schemas[name].(map[string]any)["properties"].(map[string]any)
		properties["execution_identity"] = map[string]any{"type": "string", "maxLength": 256, "description": "Operator-declared upstream identity; unverified."}
		properties["permission_summary"] = map[string]any{"type": "string", "maxLength": 4096, "description": "Operator-declared permissions; not proof of tool safety."}
	}
	schemas["ConnectionSearchDraftSpec"] = object(map[string]any{"provider_id": text(), "endpoint": text(), "credential_ref": credential}, "provider_id", "endpoint")
	for _, editing := range []bool{false, true} {
		drafts := []any{}
		for _, entry := range []struct {
			kind string
			spec map[string]any
		}{
			{"model", map[string]any{"oneOf": modelDrafts}},
			{"mcp", map[string]any{"oneOf": []any{ref("ConnectionMCPHTTPDraftSpec"), ref("ConnectionMCPStdioDraftSpec")}}},
			{"search", ref("ConnectionSearchDraftSpec")},
		} {
			properties := map[string]any{"display_name": text(), "kind": enum(entry.kind), "spec": entry.spec, "same_identity": boolean}
			required := []string{"display_name", "kind", "spec"}
			if editing {
				properties["same_identity"] = map[string]any{"type": "boolean", "const": true}
				required = append(required, "same_identity")
			}
			drafts = append(drafts, object(properties, required...))
		}
		name := "ConnectionCreateDraft"
		if editing {
			name = "ConnectionEditDraft"
		}
		schemas[name] = map[string]any{"oneOf": drafts}
		path, method := "/api/v1/connections", "post"
		if editing {
			path, method = "/api/v1/connections/{id}", "patch"
		}
		operation := paths[path].(map[string]any)[method].(map[string]any)
		operation["x-swobu-contract-stage"] = "implemented"
		operation["description"] = "Explicit typed Connection authoring; redacted read responses are not editable drafts. Local browser session and same-origin mutation admission required."
		operation["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": ref(name)}}}
		if editing {
			operation["responses"] = map[string]any{"200": response("Committed redacted Connection", ref("Connection"), "application/json"), "default": response("Canonical failure", ref("ControlProblem"), "application/problem+json")}
		} else {
			operation["responses"] = map[string]any{"201": response("Original created Connection with replay acknowledgement", ref("ConnectionCredentialReplacementResult"), "application/json"), "default": response("Canonical failure", ref("ControlProblem"), "application/problem+json")}
		}
	}

}
