package apischema

// nodeOverviewContract is the implemented secret-free whole-Node read. Its
// ETag is a whole-file content revision; it is NOT yet an accepted replacement
// for checked resource mutations or an API to replace the entire Node.
func nodeOverviewContract(document map[string]any) {
	components := document["components"].(map[string]any)
	schemas := components["schemas"].(map[string]any)
	stringField := func() map[string]any { return map[string]any{"type": "string"} }
	count := func() map[string]any { return map[string]any{"type": "integer", "minimum": 0} }
	schemas["NodeOverview"] = map[string]any{
		"type": "object", "additionalProperties": false,
		"properties": map[string]any{
			"revision":           map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"},
			"configuration_mode": map[string]any{"type": "string", "enum": []string{"managed", "externally_managed"}},
			"workspace_count":    count(), "connection_count": count(), "principal_count": count(),
			"publication_count": count(), "budget_policy_count": count(),
		},
		"required": []string{"revision", "configuration_mode", "workspace_count", "connection_count", "principal_count", "publication_count", "budget_policy_count"},
	}
	paths := document["paths"].(map[string]any)
	operation := paths["/api/v1/node"].(map[string]any)["get"].(map[string]any)
	operation["x-swobu-contract-stage"] = "implemented"
	operation["description"] = "Redacted, same-revision Node overview for local administration. ETag is the digest of complete committed swobu.yaml bytes; the response never returns YAML, secrets or operation receipts. This read is not an entire-Node mutation API."
	operation["responses"].(map[string]any)["200"] = map[string]any{
		"description": "Committed Node authority, without private material",
		"headers":     map[string]any{"ETag": map[string]any{"required": true, "description": "Strong whole-Node revision (quoted SHA-256)", "schema": stringField()}},
		"content":     map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/NodeOverview"}}},
	}
}
