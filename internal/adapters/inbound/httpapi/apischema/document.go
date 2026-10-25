package apischema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var pathParameter = regexp.MustCompile(`\{([^}]+)\}`)

// Document exports current, mounted Control operations only. Every operation
// must receive an explicit response contract before the export is accepted.
func Document() ([]byte, error) {
	paths := map[string]any{}
	for _, op := range Operations() {
		item, ok := paths[op.Path].(map[string]any)
		if !ok {
			item = map[string]any{}
			paths[op.Path] = item
		}
		parameters := []any{}
		for _, match := range pathParameter.FindAllStringSubmatch(op.Path, -1) {
			schema := map[string]any{"type": "string", "minLength": 1}
			if match[1] == "service" {
				schema["enum"] = []string{"models", "mcp", "search"}
			}
			parameters = append(parameters, map[string]any{"name": match[1], "in": "path", "required": true, "schema": schema})
		}
		if op.Method == "PATCH" || op.Method == "PUT" || op.Method == "DELETE" {
			parameters = append(parameters, map[string]any{"$ref": "#/components/parameters/IfMatch"})
		}
		if op.Method == "POST" && op.Path != "/api/v1/browser-session" && op.Path != "/api/v1/browser-launches" {
			parameters = append(parameters, map[string]any{"$ref": "#/components/parameters/IdempotencyKey"})
		}
		operation := map[string]any{
			"operationId": operationID(op), "x-swobu-contract-stage": "target-skeleton",
			"description": "Target operation. Domain request/response schemas and implementation qualification belong to its closeout card.",
			"parameters":  parameters,
			"responses":   map[string]any{"default": map[string]any{"description": "Canonical control failure", "content": map[string]any{"application/problem+json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ControlProblem"}}}}},
		}
		operation["x-swobu-plane"] = "local-control"
		if op.Path == "/api/v1/browser-session" || op.Path == "/api/v1/browser-launches" {
			operation["security"] = []any{}
			operation["x-swobu-admission"] = "single-use-launch-and-same-origin"
			if op.Path == "/api/v1/browser-launches" {
				operation["x-swobu-admission"] = "native-installation-transport-only"
			}
		}
		item[strings.ToLower(op.Method)] = operation
	}
	document := map[string]any{
		"openapi": "3.1.0", "info": map[string]any{"title": "Swobu local Control API", "version": "1.0.0", "description": "Exported implemented Node operations only; documented operations are not release-wide integration proof."},
		"paths": paths, "security": []any{map[string]any{"LocalBrowserSession": []string{}}},
		"components": map[string]any{
			"securitySchemes": map[string]any{"LocalBrowserSession": map[string]any{"type": "apiKey", "in": "cookie", "name": "swobu_operator_session", "description": "Local host browser authority. Native CLI uses installation-owned local transport; Workspace/Node credentials are never accepted as local control authority."}},
			"parameters": map[string]any{
				"IfMatch":        map[string]any{"name": "If-Match", "in": "header", "required": true, "schema": map[string]any{"type": "string", "pattern": `^"[^"\\,\s]+"$`}, "description": "One strong entity tag. Missing returns 428; stale returns 412. Comparison occurs in the owning application transaction."},
				"IdempotencyKey": map[string]any{"name": "Idempotency-Key", "in": "header", "required": true, "schema": map[string]any{"type": "string", "minLength": 1, "maxLength": 256, "pattern": "^[!-~]+$"}, "description": "Original retry identity. Durable semantic replay/conflict is owned by the application transaction."},
			},
			"schemas": map[string]any{"ControlProblem": map[string]any{
				"type": "object", "required": []string{"type", "title", "status", "code"},
				"properties": map[string]any{
					"type": map[string]any{"type": "string", "format": "uri"}, "title": map[string]any{"type": "string"}, "status": map[string]any{"type": "integer", "minimum": 400, "maximum": 599}, "code": map[string]any{"type": "string"}, "detail": map[string]any{"type": "string"}, "instance": map[string]any{"type": "string"},
					"fields":   map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
					"recovery": map[string]any{"type": "object", "required": []string{"action"}, "properties": map[string]any{"action": map[string]any{"type": "string"}}},
				},
			}},
		},
	}
	connectionContracts(document)
	workspaceServiceContracts(document)
	workspaceContracts(document)
	modelRouteContracts(document)
	serviceAuthoringContracts(document)
	nodeOverviewContract(document)
	connectActivityContracts(document)
	authorityContracts(document)
	transportContracts(document)
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func operationID(op Operation) string {
	parts := strings.FieldsFunc(op.Path, func(r rune) bool { return r == '/' || r == '-' || r == '{' || r == '}' || r == '_' })
	return fmt.Sprintf("%s_%s", strings.ToLower(op.Method), strings.Join(parts, "_"))
}
