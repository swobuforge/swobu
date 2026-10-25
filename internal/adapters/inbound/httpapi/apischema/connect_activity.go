package apischema

// connectActivityContracts exposes only mounted, proven local Connect and
// content-free Activity operations. Future managed client-setups and synthetic
// route verification remain target-skeleton until their handlers exist.
func connectActivityContracts(document map[string]any) {
	paths := document["paths"].(map[string]any)
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }
	str := func() map[string]any { return map[string]any{"type": "string"} }
	number := func() map[string]any { return map[string]any{"type": "integer", "minimum": 0} }
	arr := func(item any) map[string]any { return map[string]any{"type": "array", "items": item} }
	obj := func(fields map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false, "properties": fields, "required": required}
	}
	response := func(schema any) map[string]any {
		return map[string]any{"description": "Live local Node observation; Cache-Control: no-store", "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
	}
	promote := func(path, method, description string, schema any) map[string]any {
		op := paths[path].(map[string]any)[method].(map[string]any)
		op["x-swobu-contract-stage"] = "implemented"
		op["description"] = description
		op["responses"].(map[string]any)["200"] = response(schema)
		return op
	}
	query := func(name string, required bool, schema any) map[string]any {
		return map[string]any{"name": name, "in": "query", "required": required, "schema": schema}
	}
	boolField := func() map[string]any { return map[string]any{"type": "boolean"} }

	schemas["DetectedClient"] = obj(map[string]any{"id": str(), "name": str()}, "id", "name")
	schemas["ClientConfigurationChange"] = obj(map[string]any{
		"field": str(), "before": str(), "before_exists": boolField(), "after": str(),
	}, "field", "before", "before_exists", "after")
	schemas["ClientConfigurationAuthentication"] = obj(map[string]any{
		"principal": str(), "permission_revision": str(), "issue_credential": boolField(), "credential_id": str(),
	}, "principal", "permission_revision", "issue_credential")
	schemas["ClientConfigurationReview"] = obj(map[string]any{
		"revision": str(), "config_paths": arr(str()), "changes": arr(ref("ClientConfigurationChange")), "authentication": ref("ClientConfigurationAuthentication"),
	}, "revision", "config_paths", "changes", "authentication")
	schemas["ClientConfigurationPlan"] = obj(map[string]any{
		"revision": str(), "workspace": str(), "model_id": str(), "client": ref("DetectedClient"),
		"post_apply_hint": str(), "config_paths": arr(str()), "changes": arr(ref("ClientConfigurationChange")),
		"authentication":     ref("ClientConfigurationAuthentication"),
		"already_configured": boolField(), "requires_replace": boolField(),
	}, "revision", "workspace", "model_id", "client", "config_paths", "changes", "authentication", "already_configured", "requires_replace")
	schemas["ClientConfigurationApplyResult"] = obj(map[string]any{
		"revision": str(), "workspace": str(), "model_id": str(), "client": ref("DetectedClient"),
		"post_apply_hint": str(), "config_paths": arr(str()), "changes": arr(ref("ClientConfigurationChange")),
		"authentication":     ref("ClientConfigurationAuthentication"),
		"already_configured": boolField(), "requires_replace": boolField(), "replayed": boolField(),
	}, "revision", "workspace", "model_id", "client", "config_paths", "changes", "authentication", "already_configured", "requires_replace", "replayed")
	schemas["ClientConfigurationRequest"] = obj(map[string]any{
		"workspace": str(), "model_id": str(), "client": str(), "principal": str(), "review": ref("ClientConfigurationReview"),
	}, "workspace", "model_id", "client", "principal")
	discover := promote("/api/v1/clients", "get", "Detect locally installed clients for exactly one existing Workspace. No client files are read or changed.", arr(ref("DetectedClient")))
	discover["parameters"] = []any{query("workspace", true, str())}
	plan := promote("/api/v1/client-configurations", "post", "dry_run=true observes a client-file diff; dry_run=false requires retained exact review, strong If-Match, original retry key, and explicit human consent. Apply can leave an indeterminate marker; do not blindly resend.", map[string]any{"oneOf": []any{ref("ClientConfigurationPlan"), ref("ClientConfigurationApplyResult")}})
	plan["parameters"] = []any{
		query("dry_run", true, boolField()),
		map[string]any{"in": "header", "name": "If-Match", "required": false, "description": "Required only for apply", "schema": str()},
		map[string]any{"in": "header", "name": "Idempotency-Key", "required": false, "description": "Required only for apply (16-256 bytes)", "schema": str()},
	}
	plan["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": ref("ClientConfigurationRequest")}}}

	schemas["ActivityScope"] = obj(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"all", "workspace"}}, "workspace": str()}, "kind")
	schemas["ActivityTokenUsage"] = obj(map[string]any{
		"input_tokens": number(), "output_tokens": number(), "reasoning_tokens": number(),
		"cache_read_tokens": number(), "cache_write_tokens": number(),
	})
	schemas["ActivityExchange"] = obj(map[string]any{
		"exchange_id": str(), "access_id": str(), "workspace": str(), "client_family": str(), "client_protocol": str(),
		"operation": str(), "route": str(), "state": str(), "http_status": number(), "attempt_count": number(),
		"connection_id": str(), "connection_revision": number(), "provider": str(), "model": str(),
		"protocol": str(), "target_version": number(), "started_at": map[string]any{"type": "string", "format": "date-time"},
		"duration_ms": map[string]any{"type": "integer", "minimum": 0, "nullable": true}, "token_usage": ref("ActivityTokenUsage"),
	}, "exchange_id", "access_id", "workspace", "state", "attempt_count", "started_at")
	schemas["ActivityMCPOperation"] = obj(map[string]any{
		"operation_id": str(), "access_id": str(), "workspace": str(), "source": str(), "tool": str(),
		"started_at": map[string]any{"type": "string", "format": "date-time"},
		"settled_at": map[string]any{"type": "string", "format": "date-time"},
		"outcome":    str(), "execution_possibility": str(),
	}, "operation_id", "access_id", "workspace", "source", "tool", "started_at")
	schemas["ActivityPage"] = obj(map[string]any{"limit": number(), "mode": str(), "exchange_next": str(), "mcp_next": str()}, "limit", "mode")
	schemas["ActivityHealth"] = obj(map[string]any{
		"state": str(), "last_error": str(), "queue_depth": number(), "queue_capacity": number(),
		"offered_exchanges_total": number(), "committed_exchanges_total": number(), "dropped_exchanges_total": number(),
		"offered_mcp_operations_total": number(), "committed_mcp_operations_total": number(), "dropped_mcp_operations_total": number(),
	}, "state", "queue_depth", "queue_capacity", "offered_exchanges_total", "committed_exchanges_total", "dropped_exchanges_total", "offered_mcp_operations_total", "committed_mcp_operations_total", "dropped_mcp_operations_total")
	schemas["ActivityProjection"] = obj(map[string]any{
		"scope": ref("ActivityScope"), "exchanges": arr(ref("ActivityExchange")),
		"mcp_operations": arr(ref("ActivityMCPOperation")), "history": ref("ActivityHealth"), "page": ref("ActivityPage"),
	}, "scope", "exchanges")
	activity := promote("/api/v1/activity", "get", "Content-free bounded live snapshot, or retained history when limit is specified. Cursors are scoped to Workspace and evidence kind; absence is not proof that an execution never happened.", ref("ActivityProjection"))
	activity["parameters"] = []any{
		query("workspace", false, str()),
		query("kind", false, map[string]any{"type": "string", "enum": []string{"exchange", "mcp_operation"}}),
		query("limit", false, map[string]any{"type": "integer", "minimum": 1, "maximum": 200}),
		query("exchange_cursor", false, str()), query("mcp_cursor", false, str()),
		query("from", false, map[string]any{"type": "string", "format": "date-time"}),
		query("to", false, map[string]any{"type": "string", "format": "date-time"}),
		query("principal", false, str()),
		query("result", false, map[string]any{"type": "string", "enum": []string{"succeeded", "client_cancelled", "client_write_failed", "provider_stream_failed", "checkpoint_commit_failed", "exchange_failed"}}),
	}
	activity["description"] = "Content-free live snapshot or retained Activity page when limit is set. from/to/principal/result are available ONLY for retained kind=exchange. They filter the stored settlement history, not the live ring or MCP history. Cursors are scoped to Workspace and evidence kind, not to filters. Absence does not prove an execution never occurred."
	// Exports reuse the exact retained Activity reader and never return raw prompts,
	// completions, tool inputs or credentials. An export has a bounded row/byte
	// limit; X-Swobu-Next-Cursor continues one evidence kind, not a global snapshot.
	export := paths["/api/v1/activity/export"].(map[string]any)["get"].(map[string]any)
	export["x-swobu-contract-stage"] = "implemented"
	export["description"] = "Download a bounded retained Activity slice as NDJSON or CSV. Requires one kind (exchange or mcp_operation); up to 2000 rows, 8 MiB and 30 seconds per response. Export is content-free retained evidence, not a complete execution audit or a transactionally consistent database snapshot. Repeat with X-Swobu-Next-Cursor until absent. from/to/principal/result apply to exchange only."
	export["parameters"] = []any{
		query("workspace", false, str()),
		query("kind", true, map[string]any{"type": "string", "enum": []string{"exchange", "mcp_operation"}}),
		query("format", false, map[string]any{"type": "string", "enum": []string{"ndjson", "csv"}}),
		query("max_rows", false, map[string]any{"type": "integer", "minimum": 1, "maximum": 2000, "default": 500}),
		query("cursor", false, str()),
		query("from", false, map[string]any{"type": "string", "format": "date-time"}),
		query("to", false, map[string]any{"type": "string", "format": "date-time"}),
		query("principal", false, str()),
		query("result", false, map[string]any{"type": "string", "enum": []string{"succeeded", "client_cancelled", "client_write_failed", "provider_stream_failed", "checkpoint_commit_failed", "exchange_failed"}}),
	}
	export["responses"].(map[string]any)["200"] = map[string]any{
		"description": "Bounded retained data, with optional X-Swobu-Next-Cursor, X-Swobu-Export-Rows and Cache-Control: no-store.",
		"headers": map[string]any{
			"X-Swobu-Next-Cursor": map[string]any{"description": "Opaque continuation for the same Workspace and kind.", "schema": str()},
			"X-Swobu-Export-Rows": map[string]any{"description": "Number of records in this response.", "schema": map[string]any{"type": "integer", "minimum": 0}},
			"X-Swobu-Export-Mode": map[string]any{"description": "retained-page: not a complete or immutable database snapshot.", "schema": map[string]any{"type": "string", "enum": []string{"retained-page"}}},
		},
		"content": map[string]any{
			"application/x-ndjson": map[string]any{"schema": map[string]any{"type": "string", "description": "One tagged, redacted exchange or MCP operation JSON object per line."}},
			"text/csv":             map[string]any{"schema": map[string]any{"type": "string", "description": "Fixed-schema CSV with spreadsheet formula-safe textual values."}},
		},
	}

	schemas["ActivityInvocation"] = obj(map[string]any{
		"invocation_id": str(), "kind": str(), "started_at": str(), "settled_at": str(), "source": str(),
		"tool": str(), "provider": str(), "route": str(), "model": str(), "target": str(), "target_version": number(),
		"connection_id": str(), "connection_revision": number(), "purpose": str(),
		"outcome": str(), "execution_possibility": str(), "failure": str(),
	}, "invocation_id", "kind", "started_at")
	// Activity recovery derives only from recorded facts. These counters are
	// descriptive; they do not authorize retry or imply complete capture.
	schemas["ActivityUpstreamExecution"] = obj(map[string]any{
		"recorded_invocations": number(), "occurred": number(), "may_have_occurred": number(),
		"did_not_occur": number(), "unsettled": number(),
	}, "recorded_invocations", "occurred", "may_have_occurred", "did_not_occur", "unsettled")
	schemas["ActivityExchangeDetail"] = obj(map[string]any{
		"exchange_id": str(), "access_id": str(), "workspace": str(), "started_at": str(), "settled_at": str(),
		"delivery_result": str(), "error_code": str(), "upstream_execution": ref("ActivityUpstreamExecution"), "http_status": number(), "invocations": arr(ref("ActivityInvocation")),
	}, "exchange_id", "access_id", "workspace", "started_at", "upstream_execution", "invocations")
	schemas["ActivityOperationDetail"] = obj(map[string]any{
		"kind":     map[string]any{"type": "string", "enum": []string{"exchange", "mcp_operation"}},
		"evidence": str(), "exchange": ref("ActivityExchangeDetail"), "mcp_operation": ref("ActivityMCPOperation"),
	}, "kind", "evidence")
	promote("/api/v1/activity/operations/{id}", "get", "Content-free causal evidence and separate upstream/client outcomes for one exact operation. Counts describe recorded invocations, not proof against unrecorded execution or permission to retry. A 404 means unavailable evidence, not confirmed absence or failure.", ref("ActivityOperationDetail"))
}
