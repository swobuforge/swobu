package responses

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func decodeResponsesTools(tools []responsesToolDefinitionDTO, subjectPrefix string, feature compat.Feature, sink compat.Sink, exchangeID string) ([]canonical.ToolDeclaration, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]canonical.ToolDeclaration, 0, len(tools))
	seen := map[string]struct{}{}
	for idx, tool := range tools {
		decls, err := decodeResponsesToolNode(tool, responsesToolNamespaceContext{subjectPrefix: subjectPrefix, index: idx}, feature, sink, exchangeID)
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
	return out, nil
}

// swobu:lint ignore string-switch because=protocol boundary decodes Responses tool variants.
func decodeResponsesToolNode(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, feature compat.Feature, sink compat.Sink, exchangeID string) ([]canonical.ToolDeclaration, error) {
	kind := strings.ToLower(strings.TrimSpace(tool.Type)) // swobu:io-string source=domain
	if feature == compat.ResponseItemsKind {
		if err := admitResponsesProviderOutputChild(kind); err != nil {
			return nil, err
		}
	}
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
		return decodeResponsesNamespaceTool(tool, ctx, feature, sink, exchangeID)
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
	case "tool_search":
		if len(ctx.path) > 0 {
			return nil, canonical.BadRequest("responses tool_search cannot be nested")
		}
		schema, err := responsesToolParametersFromWire(tool.Parameters)
		if err != nil {
			return nil, err
		}
		executor := canonical.DiscoveryExecutorProvider
		switch strings.TrimSpace(tool.Execution) {
		case "client":
			executor = canonical.DiscoveryExecutorClient
		case "server", "":
		default:
			return nil, canonical.BadRequest("responses tool_search execution is invalid")
		}
		decl, err := canonical.NewToolDiscoveryTool(tool.Description, schema, executor)
		if err != nil {
			return nil, canonical.BadRequest("responses tool_search declaration is invalid")
		}
		return []canonical.ToolDeclaration{decl}, nil
	case "web_search", "web_search_preview":
		if len(ctx.path) > 0 {
			return nil, canonical.BadRequest("responses web-search tool cannot be nested or renamed")
		}
		decl, present, err := decodeResponsesWebSearchTool(tool, ctx, feature, sink, exchangeID)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, nil
		}
		return []canonical.ToolDeclaration{decl}, nil
	default:
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, feature, compat.Drop, responsesToolNodeSubject(ctx)); err != nil {
			return nil, err
		}
		return nil, nil
	}
}

func decodeResponsesWebSearchTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, feature compat.Feature, sink compat.Sink, exchangeID string) (canonical.ToolDeclaration, bool, error) {
	if strings.TrimSpace(tool.Name) != "" || len(tool.Tools) > 0 || len(tool.Parameters) > 0 || len(tool.Format) > 0 { // swobu:io-string source=boundary
		return canonical.ToolDeclaration{}, false, canonical.BadRequest("responses web-search tool has invalid declaration fields")
	}
	if len(tool.SearchContentTypes) > 0 {
		for _, contentType := range tool.SearchContentTypes {
			switch strings.TrimSpace(contentType) {
			case "text":
			default:
				return dropResponsesWebSearchOperation(ctx, feature, sink, exchangeID)
			}
		}
	}
	if strings.TrimSpace(tool.OutputFormat) != "" { // swobu:io-string source=boundary
		return dropResponsesWebSearchOperation(ctx, feature, sink, exchangeID)
	}
	if tool.ExternalWebAccess != nil {
		if !*tool.ExternalWebAccess {
			return dropResponsesWebSearchOperation(ctx, feature, sink, exchangeID)
		}
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, feature, compat.Drop, responsesToolFieldSubject(ctx, "external_web_access")); err != nil {
			return canonical.ToolDeclaration{}, false, err
		}
	}
	if tool.Filters != nil {
		for _, values := range [][]string{tool.Filters.AllowedDomains, tool.Filters.BlockedDomains} {
			for _, value := range values {
				if strings.TrimSpace(value) == "" {
					return canonical.ToolDeclaration{}, false, canonical.BadRequest("responses web-search domain filter is invalid")
				}
			}
			if len(values) > 0 {
				return dropResponsesWebSearchOperation(ctx, feature, sink, exchangeID)
			}
		}
	}
	if tool.UserLocation != nil {
		if kind := strings.TrimSpace(tool.UserLocation.Type); kind != "approximate" { // swobu:io-string source=boundary
			return dropResponsesWebSearchOperation(ctx, feature, sink, exchangeID)
		}
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, feature, compat.Approx, responsesToolFieldSubject(ctx, "user_location")); err != nil {
			return canonical.ToolDeclaration{}, false, err
		}
	}
	if raw := strings.TrimSpace(tool.SearchContextSize); raw != "" { // swobu:io-string source=boundary
		if err := emitResponsesCompatibilityDecision(sink, exchangeID, feature, compat.Approx, responsesToolFieldSubject(ctx, "search_context_size")); err != nil {
			return canonical.ToolDeclaration{}, false, err
		}
	}
	return canonical.NewWebSearchDeclaration(), true, nil
}

func dropResponsesWebSearchOperation(ctx responsesToolNamespaceContext, feature compat.Feature, sink compat.Sink, exchangeID string) (canonical.ToolDeclaration, bool, error) {
	if err := emitResponsesCompatibilityDecision(sink, exchangeID, feature, compat.Drop, responsesToolNodeSubject(ctx)); err != nil {
		return canonical.ToolDeclaration{}, false, err
	}
	return canonical.ToolDeclaration{}, false, nil
}

func decodeResponsesNamespaceTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, feature compat.Feature, sink compat.Sink, exchangeID string) ([]canonical.ToolDeclaration, error) {
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("responses request tool namespace declarations require a name")
	}
	nextCtx := responsesToolNamespaceContext{
		path:          append(append([]string(nil), ctx.path...), name),
		subjectPrefix: ctx.subjectPrefix + "/" + strconv.Itoa(ctx.index) + "/tools",
	}
	if len(tool.Tools) == 0 {
		return nil, canonical.BadRequest("responses request tool namespace declarations require child tools")
	}
	out := make([]canonical.ToolDeclaration, 0, len(tool.Tools))
	for idx, child := range tool.Tools {
		decls, err := decodeResponsesToolNode(child, responsesToolNamespaceContext{path: nextCtx.path, subjectPrefix: nextCtx.subjectPrefix, index: idx}, feature, sink, exchangeID)
		if err != nil {
			return nil, err
		}
		out = append(out, decls...)
	}
	if len(out) == 0 {
		return nil, nil
	}
	key, err := canonical.NewRequestToolKey(canonical.ToolKindNamespace, strings.Join(nextCtx.path, "/"))
	if err != nil {
		return nil, err
	}
	namespace, err := canonical.NewToolNamespace(key, tool.Description, out)
	if err != nil {
		return nil, err
	}
	return []canonical.ToolDeclaration{namespace}, nil
}

func responsesToolNodeSubject(ctx responsesToolNamespaceContext) compat.Subject {
	return compat.Subject(ctx.subjectPrefix + "/" + strconv.Itoa(ctx.index) + "/type")
}

func responsesToolFieldSubject(ctx responsesToolNamespaceContext, field string) compat.Subject {
	return compat.Subject(ctx.subjectPrefix + "/" + strconv.Itoa(ctx.index) + "/" + field)
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
	return canonical.NewFunctionTool(key, tool.Description, schema, strict)
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
	return canonical.NewCustomTool(key, tool.Description, format)
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
