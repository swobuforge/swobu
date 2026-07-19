package responses

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func decodeResponsesTools(tools []responsesToolDefinitionDTO, sink compat.Sink, exchangeID string) ([]canonical.ToolDecl, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]canonical.ToolDecl, 0, len(tools))
	seen := map[string]struct{}{}
	for idx, tool := range tools {
		decls, err := decodeResponsesToolNode(tool, responsesToolNamespaceContext{index: idx}, sink, exchangeID)
		if err != nil {
			return nil, err
		}
		for _, decl := range decls {
			if decl == nil {
				continue
			}
			id := decl.ToolID().String()
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
func decodeResponsesToolNode(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, sink compat.Sink, exchangeID string) ([]canonical.ToolDecl, error) {
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
			return []canonical.ToolDecl{decl}, nil
		}
		decl, err := decodeResponsesFlatFunctionTool(tool, ctx, sink, exchangeID)
		if err != nil {
			return nil, err
		}
		return []canonical.ToolDecl{decl}, nil
	case "custom":
		if len(ctx.path) > 0 {
			decl, err := decodeResponsesNestedCustomTool(tool, ctx)
			if err != nil {
				return nil, err
			}
			return []canonical.ToolDecl{decl}, nil
		}
		decl, err := decodeResponsesFlatCustomTool(tool, ctx, sink, exchangeID)
		if err != nil {
			return nil, err
		}
		return []canonical.ToolDecl{decl}, nil
	case "web_search", "image_generation", "tool_search":
		if len(ctx.path) > 0 {
			logResponsesNamespaceChildWarning(tool, ctx, kind)
			return nil, nil
		}
		decl, err := decodeResponsesCapabilityTool(tool, kind)
		if err != nil {
			return nil, err
		}
		return []canonical.ToolDecl{decl}, nil
	default:
		if len(ctx.path) > 0 {
			logResponsesNamespaceChildWarning(tool, ctx, kind)
			return nil, nil
		}
		return nil, canonical.BadRequest("responses request contains an unsupported tool type")
	}
}

func decodeResponsesNamespaceTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, sink compat.Sink, exchangeID string) ([]canonical.ToolDecl, error) {
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("responses request tool namespace declarations require a name")
	}
	nextCtx := responsesToolNamespaceContext{
		path:         append(append([]string(nil), ctx.path...), name),
		descriptions: append(append([]string(nil), ctx.descriptions...), strings.TrimSpace(tool.Description)), // swobu:io-string source=domain
		execution:    mergeResponsesExecution(ctx.execution, tool.Execution),
	}
	if len(tool.Tools) == 0 {
		return nil, nil
	}
	out := make([]canonical.ToolDecl, 0, len(tool.Tools))
	for idx, child := range tool.Tools {
		decls, err := decodeResponsesToolNode(child, responsesToolNamespaceContext{path: nextCtx.path, descriptions: nextCtx.descriptions, execution: nextCtx.execution, index: idx}, sink, exchangeID)
		if err != nil {
			return nil, err
		}
		out = append(out, decls...)
	}
	return out, nil
}

func decodeResponsesFlatFunctionTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, sink compat.Sink, exchangeID string) (canonical.ToolDecl, error) {
	schema, err := responsesToolParametersFromWire(tool.Parameters)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("responses request tool declarations require a name")
	}
	id, leaf, err := responsesFlatToolIdentityFromWire(name, canonical.ToolKindFunction, "tools[].name", "function")
	if err != nil {
		return nil, err
	}
	if _, _, projected := canonical.ToolIdentityFromWire(name, canonical.ToolKindFunction); projected {
		if err := emitResponsesToolNameNamespaceDecision(sink, exchangeID, nil, compat.Exact, compat.Subject("wire:/tools/"+strconv.Itoa(ctx.index)+"/name")); err != nil {
			return nil, err
		}
	}
	decl := canonical.NewFunctionToolDecl(id.String(), leaf, tool.Description, schema)
	decl.Strict = cloneBoolPointer(tool.Strict)
	decl.Execution = normalizeToolExecutionOwner(tool.Execution)
	return decl, nil
}

func decodeResponsesNestedFunctionTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext) (canonical.ToolDecl, error) {
	schema, err := responsesToolParametersFromWire(tool.Parameters)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("responses request tool declarations require a name")
	}
	path := append(append([]string(nil), ctx.path...), name)
	decl := canonical.NewFunctionToolDecl(canonical.NewSemanticToolIDFor(canonical.ToolOriginRequest, canonical.ToolKindFunction, strings.Join(path, "/")).String(), name, composeResponsesToolDescription(ctx.descriptions, tool.Description), schema)
	decl.Strict = cloneBoolPointer(tool.Strict)
	decl.Execution = normalizeToolExecutionOwner(mergeResponsesExecution(ctx.execution, tool.Execution))
	return decl, nil
}

func decodeResponsesFlatCustomTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, sink compat.Sink, exchangeID string) (canonical.ToolDecl, error) {
	format, err := responsesToolFormatFromWire(tool.Format)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("responses request tool declarations require a name")
	}
	id, leaf, err := responsesFlatToolIdentityFromWire(name, canonical.ToolKindCustom, "tools[].name", "custom")
	if err != nil {
		return nil, err
	}
	if _, _, projected := canonical.ToolIdentityFromWire(name, canonical.ToolKindCustom); projected {
		if err := emitResponsesToolNameNamespaceDecision(sink, exchangeID, nil, compat.Exact, compat.Subject("wire:/tools/"+strconv.Itoa(ctx.index)+"/name")); err != nil {
			return nil, err
		}
	}
	decl := canonical.NewCustomToolDecl(id.String(), leaf, tool.Description, format)
	decl.Execution = normalizeToolExecutionOwner(tool.Execution)
	return decl, nil
}

func decodeResponsesNestedCustomTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext) (canonical.ToolDecl, error) {
	format, err := responsesToolFormatFromWire(tool.Format)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("responses request tool declarations require a name")
	}
	path := append(append([]string(nil), ctx.path...), name)
	decl := canonical.NewCustomToolDecl(canonical.NewSemanticToolIDFor(canonical.ToolOriginRequest, canonical.ToolKindCustom, strings.Join(path, "/")).String(), name, composeResponsesToolDescription(ctx.descriptions, tool.Description), format)
	decl.Execution = normalizeToolExecutionOwner(mergeResponsesExecution(ctx.execution, tool.Execution))
	return decl, nil
}

func decodeResponsesCapabilityTool(tool responsesToolDefinitionDTO, capability string) (canonical.ToolDecl, error) {
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		name = capability
	}
	config, err := responsesToolCapabilityConfigFromWire(tool, capability)
	if err != nil {
		return nil, err
	}
	decl := canonical.NewCapabilityToolDecl(name, canonical.NewToolCapability(capability), config)
	decl.Execution = normalizeToolExecutionOwner(tool.Execution)
	return decl, nil
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

func mergeResponsesExecution(parent, child string) string {
	if trimmed := strings.TrimSpace(child); trimmed != "" { // swobu:io-string source=domain
		return trimmed
	}
	return strings.TrimSpace(parent) // swobu:io-string source=domain
}

func logResponsesNamespaceChildWarning(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext, kind string) {
	slog.Warn("responses namespace child tool skipped",
		"component", "protocol.responses",
		"event", "namespace_child_skipped",
		"namespace_path", strings.Join(ctx.path, "/"),
		"tool_type", kind,
		"tool_name", strings.TrimSpace(tool.Name), // swobu:io-string source=boundary
	)
}

// swobu:lint ignore string-switch because=protocol boundary decodes Responses capability variants.
func responsesToolCapabilityConfigFromWire(tool responsesToolDefinitionDTO, capability string) (canonical.ToolCapabilityConfig, error) {
	obj := map[string]any{}
	switch capability {
	case "web_search":
		if tool.ExternalWebAccess != nil {
			obj["external_web_access"] = *tool.ExternalWebAccess
		}
		if len(tool.SearchContentTypes) > 0 {
			obj["search_content_types"] = tool.SearchContentTypes
		}
	case "image_generation":
		if strings.TrimSpace(tool.OutputFormat) != "" { // swobu:io-string source=domain
			obj["output_format"] = strings.TrimSpace(tool.OutputFormat) // swobu:io-string source=domain
		}
	case "tool_search":
		trimmed := strings.TrimSpace(string(tool.Parameters)) // swobu:io-string source=boundary
		if trimmed == "" || trimmed == "null" {
			return canonical.ToolCapabilityConfig{}, canonical.BadRequest("responses request tool_search declarations require parameters")
		}
		if _, err := sse.DecodeJSONObject(json.RawMessage(trimmed), "responses request tool_search declarations require parameters"); err != nil {
			return canonical.ToolCapabilityConfig{}, err
		}
		obj["parameters"] = json.RawMessage(trimmed)
	default:
		return canonical.ToolCapabilityConfig{}, canonical.BadRequest("responses request contains an unsupported tool type")
	}
	if len(obj) == 0 {
		return canonical.EmptyToolCapabilityConfig(), nil
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return canonical.ToolCapabilityConfig{}, canonical.InternalError("responses request tool capability config could not be encoded")
	}
	return canonical.NewToolCapabilityConfigObject(string(normalized)), nil
}

func responsesToolFormatFromWire(raw json.RawMessage) (canonical.ToolFormat, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return canonical.ToolFormat{}, canonical.BadRequest("responses request tool declarations require format")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(trimmed), "responses request tool declaration format is invalid"); err != nil {
		return canonical.ToolFormat{}, err
	}
	return canonical.NewToolFormatObject(trimmed), nil
}

func responsesToolParametersFromWire(raw json.RawMessage) (canonical.ToolSchema, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return canonical.ToolSchema{}, canonical.BadRequest("responses request tool declarations require parameters")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(trimmed), "responses request tool declaration parameters are invalid"); err != nil {
		return canonical.ToolSchema{}, err
	}
	return canonical.NewToolSchemaObject(trimmed), nil
}
