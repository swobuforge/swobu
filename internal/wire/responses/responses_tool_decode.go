package responses

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decodeResponsesTools(tools []responsesToolDefinitionDTO, sink compat.Sink, exchangeID string) ([]canonical.ToolDeclaration, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]canonical.ToolDeclaration, 0, len(tools))
	seen := map[string]struct{}{}
	for idx, tool := range tools {
		decls, err := decodeResponsesToolNode(tool, responsesToolNamespaceContext{index: idx}, sink, exchangeID)
		if err != nil {
			return nil, err
		}
		for _, decl := range decls {
			id := decl.Key().String()
			if _, ok := seen[id]; ok {
				return nil, canonical.BadRequest("responses request tool declarations are ambiguous")
			}
			seen[id] = struct{}{}
			out = append(out, decl)
		}
	}
	if len(out) == 0 {
		return nil, canonical.UnsupportedOperation("responses request namespace declarations contain no supported child tools")
	}
	return out, nil
}

// swobu:lint ignore string-switch because=protocol boundary decodes Responses tool variants.
func decodeResponsesToolNode(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, sink compat.Sink, exchangeID string) ([]canonical.ToolDeclaration, error) {
	kind := strings.ToLower(strings.TrimSpace(tool.Type)) // swobu:io-string source=domain
	if kind == "" {
		switch {
		case len(tool.Tools) > 0:
			kind = "namespace"
		case len(tool.Format) > 0:
			kind = "custom"
		default:
			kind = "function"
		}
	}
	switch kind {
	case "namespace":
		return decodeResponsesNamespaceTool(tool, ctx, sink, exchangeID)
	case "function":
		if len(ctx.path) > 0 {
			decl, err := decodeResponsesNestedFunctionTool(tool, ctx)
			if err != nil {
				return nil, err
			}
			return []canonical.ToolDeclaration{decl}, nil
		}
		decl, err := decodeResponsesFlatFunctionTool(tool, ctx, sink, exchangeID)
		if err != nil {
			return nil, err
		}
		return []canonical.ToolDeclaration{decl}, nil
	case "custom":
		if len(ctx.path) > 0 {
			decl, err := decodeResponsesNestedCustomTool(tool, ctx)
			if err != nil {
				return nil, err
			}
			return []canonical.ToolDeclaration{decl}, nil
		}
		decl, err := decodeResponsesFlatCustomTool(tool, ctx, sink, exchangeID)
		if err != nil {
			return nil, err
		}
		return []canonical.ToolDeclaration{decl}, nil
	case "web_search", "web_search_preview":
		if len(ctx.path) > 0 {
			return nil, canonical.BadRequest("responses web-search tool cannot be nested or renamed")
		}
		decl, err := decodeResponsesWebSearchTool(tool, ctx.index, sink, exchangeID)
		if err != nil {
			return nil, err
		}
		return []canonical.ToolDeclaration{decl}, nil
	default:
		return nil, canonical.BadRequest("responses request contains an unsupported tool type")
	}
}

func decodeResponsesWebSearchTool(tool responsesToolDefinitionDTO, index int, sink compat.Sink, exchangeID string) (canonical.ToolDeclaration, error) {
	if strings.TrimSpace(tool.Name) != "" || len(tool.Tools) > 0 || len(tool.Parameters) > 0 || len(tool.Format) > 0 { // swobu:io-string source=boundary
		return canonical.ToolDeclaration{}, canonical.BadRequest("responses web-search tool has invalid declaration fields")
	}
	if len(tool.SearchContentTypes) > 0 {
		for _, contentType := range tool.SearchContentTypes {
			if strings.EqualFold(strings.TrimSpace(contentType), "image") {
				return canonical.ToolDeclaration{}, canonical.UnsupportedOperation("responses image-search intent is not supported")
			}
		}
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestTools, compat.Drop, compat.Subject(fmt.Sprintf("wire:/tools/%d/search_content_types", index))); err != nil {
			return canonical.ToolDeclaration{}, err
		}
	}
	if strings.TrimSpace(tool.OutputFormat) != "" { // swobu:io-string source=boundary
		return canonical.ToolDeclaration{}, canonical.UnsupportedOperation("responses image-search output format is not supported")
	}
	if tool.ExternalWebAccess != nil {
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestTools, compat.Drop, compat.Subject(fmt.Sprintf("wire:/tools/%d/external_web_access", index))); err != nil {
			return canonical.ToolDeclaration{}, err
		}
	}
	if tool.Filters != nil {
		for field, values := range map[string][]string{"allowed_domains": tool.Filters.AllowedDomains, "blocked_domains": tool.Filters.BlockedDomains} {
			for _, value := range values {
				if strings.TrimSpace(value) == "" {
					return canonical.ToolDeclaration{}, canonical.BadRequest("responses web-search domain filter is invalid")
				}
			}
			if len(values) > 0 {
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestTools, compat.Drop, compat.Subject(fmt.Sprintf("wire:/tools/%d/filters/%s", index, field))); err != nil {
					return canonical.ToolDeclaration{}, err
				}
			}
		}
	}
	if tool.UserLocation != nil {
		if kind := strings.TrimSpace(tool.UserLocation.Type); kind != "" && kind != "approximate" {
			return canonical.ToolDeclaration{}, canonical.BadRequest("responses web-search user_location type is invalid")
		}
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestTools, compat.Drop, compat.Subject(fmt.Sprintf("wire:/tools/%d/user_location", index))); err != nil {
			return canonical.ToolDeclaration{}, err
		}
	}
	if raw := strings.TrimSpace(tool.SearchContextSize); raw != "" { // swobu:io-string source=boundary
		if raw != "low" && raw != "medium" && raw != "high" {
			return canonical.ToolDeclaration{}, canonical.BadRequest("responses web-search search_context_size is invalid")
		}
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestTools, compat.Drop, compat.Subject(fmt.Sprintf("wire:/tools/%d/search_context_size", index))); err != nil {
			return canonical.ToolDeclaration{}, err
		}
	}
	return canonical.NewWebSearchDeclaration(), nil
}

func decodeResponsesNamespaceTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, sink compat.Sink, exchangeID string) ([]canonical.ToolDeclaration, error) {
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("responses request tool namespace declarations require a name")
	}
	nextCtx := responsesToolNamespaceContext{
		path:         append(append([]string(nil), ctx.path...), name),
		descriptions: append(append([]string(nil), ctx.descriptions...), strings.TrimSpace(tool.Description)), // swobu:io-string source=domain
	}
	if len(tool.Tools) == 0 {
		return nil, nil
	}
	out := make([]canonical.ToolDeclaration, 0, len(tool.Tools))
	for idx, child := range tool.Tools {
		decls, err := decodeResponsesToolNode(child, responsesToolNamespaceContext{path: nextCtx.path, descriptions: nextCtx.descriptions, index: idx}, sink, exchangeID)
		if err != nil {
			return nil, err
		}
		out = append(out, decls...)
	}
	return out, nil
}

func decodeResponsesFlatFunctionTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, sink compat.Sink, exchangeID string) (canonical.ToolDeclaration, error) {
	schema, err := responsesToolParametersFromWire(tool.Parameters)
	if err != nil {
		return canonical.ToolDeclaration{}, err
	}
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return canonical.ToolDeclaration{}, canonical.BadRequest("responses request tool declarations require a name")
	}
	id, _, err := responsesFlatToolIdentityFromWire(name, canonical.ToolKindFunction, "tools[].name", "function")
	if err != nil {
		return canonical.ToolDeclaration{}, err
	}
	strict := canonical.Unspecified[bool]()
	if tool.Strict != nil {
		strict = canonical.Specify(*tool.Strict)
	}
	return canonical.NewFunctionTool(id, tool.Description, schema, strict)
}

func decodeResponsesNestedFunctionTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext) (canonical.ToolDeclaration, error) {
	schema, err := responsesToolParametersFromWire(tool.Parameters)
	if err != nil {
		return canonical.ToolDeclaration{}, err
	}
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return canonical.ToolDeclaration{}, canonical.BadRequest("responses request tool declarations require a name")
	}
	path := append(append([]string(nil), ctx.path...), name)
	strict := canonical.Unspecified[bool]()
	if tool.Strict != nil {
		strict = canonical.Specify(*tool.Strict)
	}
	key, err := canonical.NewRequestToolKey(canonical.ToolKindFunction, strings.Join(path, "/"))
	if err != nil {
		return canonical.ToolDeclaration{}, err
	}
	return canonical.NewFunctionTool(key, composeResponsesToolDescription(ctx.descriptions, tool.Description), schema, strict)
}

func decodeResponsesFlatCustomTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, sink compat.Sink, exchangeID string) (canonical.ToolDeclaration, error) {
	format, err := responsesToolFormatFromWire(tool.Format)
	if err != nil {
		return canonical.ToolDeclaration{}, err
	}
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return canonical.ToolDeclaration{}, canonical.BadRequest("responses request tool declarations require a name")
	}
	id, _, err := responsesFlatToolIdentityFromWire(name, canonical.ToolKindCustom, "tools[].name", "custom")
	if err != nil {
		return canonical.ToolDeclaration{}, err
	}
	return canonical.NewCustomTool(id, tool.Description, format)
}

func decodeResponsesNestedCustomTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext) (canonical.ToolDeclaration, error) {
	format, err := responsesToolFormatFromWire(tool.Format)
	if err != nil {
		return canonical.ToolDeclaration{}, err
	}
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return canonical.ToolDeclaration{}, canonical.BadRequest("responses request tool declarations require a name")
	}
	path := append(append([]string(nil), ctx.path...), name)
	key, err := canonical.NewRequestToolKey(canonical.ToolKindCustom, strings.Join(path, "/"))
	if err != nil {
		return canonical.ToolDeclaration{}, err
	}
	return canonical.NewCustomTool(key, composeResponsesToolDescription(ctx.descriptions, tool.Description), format)
}

func composeResponsesToolDescription(parts []string, extra string) string {
	values := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" { // swobu:io-string source=domain
			values = append(values, trimmed)
		}
	}
	if trimmed := strings.TrimSpace(extra); trimmed != "" { // swobu:io-string source=domain
		values = append(values, trimmed)
	}
	return strings.Join(values, "\n\n")
}

func responsesToolFormatFromWire(raw json.RawMessage) (canonical.ToolFormat, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return canonical.ToolFormat{}, canonical.BadRequest("responses request tool declarations require format")
	}
	object, err := canonical.ParseJSONObject([]byte(trimmed))
	if err != nil {
		return canonical.ToolFormat{}, err
	}
	return canonical.NewToolFormatObject(object), nil
}

func responsesToolParametersFromWire(raw json.RawMessage) (canonical.ToolSchema, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return canonical.ToolSchema{}, canonical.BadRequest("responses request tool declarations require parameters")
	}
	object, err := canonical.ParseJSONObject([]byte(trimmed))
	if err != nil {
		return canonical.ToolSchema{}, err
	}
	return canonical.NewToolSchemaObject(object), nil
}
