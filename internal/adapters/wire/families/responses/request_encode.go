// swobu:lint ignore file-length because=responses request codec owns nested tool lowering fanout.
package responses

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type EncodeOptions struct {
	Instructions         string
	ForceStructuredInput bool
	Store                *bool
}

type inputMessageItem struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Status  string `json:"status,omitempty"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

type functionCallItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type functionCallOutputItem struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.WireDocument, error) {
	return encodeCarrierWithOptions(req, d, EncodeOptions{})
}

func encodeCarrierWithOptions(req canonical.CanonicalRequest, d delivery.Delivery, options EncodeOptions) (carrier.WireDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return carrier.WireDocument{}, canonical.UnsupportedDelivery("response requests do not implement the requested delivery mode on the responses protocol")
	}

	tools := req.Tools()
	input, err := encodeInput(req, options.ForceStructuredInput)
	if err != nil {
		return carrier.WireDocument{}, err
	}
	logResponsesEncodeShape(req, input, d)

	payload := map[string]any{
		"model": req.Model(),
	}
	if input != nil {
		payload["input"] = input
	}
	if trimmed := strings.TrimSpace(options.Instructions); trimmed != "" { // swobu:io-string source=boundary
		payload["instructions"] = trimmed
	}
	if choice, err := encodeToolChoice(req.ToolPolicy(), tools); err != nil {
		return carrier.WireDocument{}, err
	} else if choice != nil {
		payload["tool_choice"] = choice
	}
	if wireTools, err := encodeResponsesTools(tools); err != nil {
		return carrier.WireDocument{}, err
	} else if len(wireTools) > 0 {
		payload["tools"] = wireTools
	}
	if err := encodeResponsesToolCallBatch(payload, req.ToolCallBatch(), len(tools) > 0); err != nil {
		return carrier.WireDocument{}, err
	}
	if err := encodeResponsesGenerationControls(payload, req.Controls()); err != nil {
		return carrier.WireDocument{}, err
	}
	if text, err := encodeResponsesOutputFormat(req.OutputFormat()); err != nil {
		return carrier.WireDocument{}, err
	} else if text != nil {
		payload["text"] = text
	}
	if prev, ok := req.Turn().PreviousID(); ok {
		payload["previous_response_id"] = prev
	}
	if options.Store != nil {
		payload["store"] = *options.Store
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return carrier.WireDocument{}, canonical.BadRequest("response request could not be encoded for the responses protocol")
	}

	return carrier.NewWireDocument(
		carrier.StageProviderRequestOut,
		"",
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	), nil
}

func logResponsesEncodeShape(req canonical.CanonicalRequest, input any, d delivery.Delivery) {
	thread := req.Items()
	encodedItems := thread
	if !req.Turn().IsZero() {
		encodedItems = canonical.CurrentTurnDelta(thread)
	}
	inputType := "nil"
	if input != nil {
		switch input.(type) {
		case string:
			inputType = "string"
		case []any:
			inputType = "array"
		default:
			inputType = "other"
		}
	}
	slog.Debug("responses encode",
		"component", "protocol.responses",
		"event", "outbound_request_shape",
		"streaming", d.Mode == delivery.Streaming,
		"has_previous_response_id", !req.Turn().IsZero(), // swobu:io-string source=boundary
		"thread_item_count", len(thread),
		"encoded_item_count", len(encodedItems),
		"thread_tail_role", responsesTailRole(thread),
		"encoded_tail_role", responsesTailRole(encodedItems),
		"input_type", inputType,
	)
}

func responsesTailRole(items []canonical.CanonicalItem) string {
	if len(items) == 0 {
		return ""
	}
	tailAuthor := items[len(items)-1].Author
	if tailAuthor == canonical.ItemAuthorAssistant {
		return "assistant"
	}
	if tailAuthor == canonical.ItemAuthorTool {
		return "tool"
	}
	return "user"
}

func encodeResponsesTools(tools []canonical.ToolDecl) ([]any, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		wire, err := encodeResponsesTool(tool)
		if err != nil {
			return nil, err
		}
		out = append(out, wire)
	}
	return out, nil
}

func encodeResponsesTool(tool canonical.ToolDecl) (map[string]any, error) {
	if tool == nil {
		return nil, canonical.BadRequest("response request tool declarations are invalid")
	}
	switch decl := tool.(type) {
	case canonical.FunctionToolDecl:
		return encodeResponsesFunctionToolDecl(decl)
	case *canonical.FunctionToolDecl:
		return encodeResponsesFunctionToolDecl(*decl)
	case canonical.CustomToolDecl:
		return encodeResponsesCustomToolDecl(decl)
	case *canonical.CustomToolDecl:
		return encodeResponsesCustomToolDecl(*decl)
	case canonical.CapabilityToolDecl:
		return encodeResponsesCapabilityToolDecl(decl)
	case *canonical.CapabilityToolDecl:
		return encodeResponsesCapabilityToolDecl(*decl)
	default:
		return nil, canonical.UnsupportedOperation("responses protocol only supports known tool declaration types")
	}
}

func encodeResponsesFunctionToolDecl(decl canonical.FunctionToolDecl) (map[string]any, error) {
	name, err := responsesToolWireName(decl)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("response request tool declarations require a name")
	}
	parameters, err := responsesToolParametersFromSchema(decl.ToolInputSchema())
	if err != nil {
		return nil, err
	}
	wire := map[string]any{
		"type":        "function",
		"name":        name,
		"description": strings.TrimSpace(decl.ToolDescription()), // swobu:io-string source=boundary
		"parameters":  parameters,
	}
	if decl.Strict != nil {
		wire["strict"] = *decl.Strict
	}
	return wire, nil
}

func encodeResponsesCustomToolDecl(decl canonical.CustomToolDecl) (map[string]any, error) {
	name, err := responsesToolWireName(decl)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("response request custom tool declarations require a name")
	}
	format, err := responsesToolFormatFromCanonical(decl.Format)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"type":        "custom",
		"name":        name,
		"description": strings.TrimSpace(decl.ToolDescription()), // swobu:io-string source=boundary
		"format":      format,
	}, nil
}

func encodeResponsesCapabilityToolDecl(decl canonical.CapabilityToolDecl) (map[string]any, error) {
	capability := strings.TrimSpace(string(decl.ToolCapability())) // swobu:io-string source=boundary
	if capability == "" {
		return nil, canonical.BadRequest("response request tool declarations require a capability")
	}
	wire := map[string]any{
		"type": capability,
	}
	if decl.Config.RawObject() != "" {
		config, err := responsesToolConfigFromCanonical(decl.CapabilityConfig())
		if err != nil {
			return nil, err
		}
		for key, value := range config {
			wire[key] = value
		}
	}
	if execution := strings.TrimSpace(string(decl.Owner())); execution != "" && execution != "client" {
		wire["execution"] = execution
	}
	if capability == "tool_search" {
		// Preserve the observed tool_search shape even when the owner remains
		// client; the backend still needs the explicit execution hint.
		if _, ok := wire["execution"]; !ok {
			wire["execution"] = strings.TrimSpace(string(decl.Owner()))
		}
	}
	return wire, nil
}

func responsesToolParametersFromSchema(schema canonical.ToolSchema) (any, error) {
	raw := strings.TrimSpace(schema.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, canonical.BadRequest("response request tool declarations require input_schema")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(raw), "response request tool declaration input_schema is invalid"); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func responsesToolFormatFromCanonical(format canonical.ToolFormat) (any, error) {
	raw := strings.TrimSpace(format.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, canonical.BadRequest("response request tool declarations require format")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(raw), "response request tool declaration format is invalid"); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func responsesToolConfigFromCanonical(config canonical.ToolCapabilityConfig) (map[string]any, error) {
	raw := strings.TrimSpace(config.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return map[string]any{}, nil
	}
	obj, err := sse.DecodeJSONObject(json.RawMessage(raw), "response request tool capability config is invalid")
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func encodeToolChoice(policy canonical.ToolPolicy, tools []canonical.ToolDecl) (any, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	switch policy.Mode {
	case canonical.ToolPolicyNone:
		return "none", nil
	case canonical.ToolPolicyAuto:
		return "auto", nil
	case canonical.ToolPolicyRequired:
		if len(tools) == 0 {
			return nil, canonical.BadRequest("response request tool_choice required requires at least one tool")
		}
		return "required", nil
	case canonical.ToolPolicySpecific:
		specific, ok := policy.SpecificID()
		if !ok {
			return nil, canonical.BadRequest("response request tool_choice specific requires a tool id")
		}
		specificType, _ := policy.SpecificToolType()
		decl, resolvedType, err := canonical.ResolveToolDeclByID(tools, specific, specificType)
		if err != nil {
			return nil, err
		}
		name, err := responsesToolWireName(decl)
		if err != nil {
			return nil, err
		}
		if resolvedType == "" {
			resolvedType = "function"
		}
		return map[string]any{
			"type": resolvedType,
			"name": name,
		}, nil
	default:
		return nil, canonical.BadRequest("response request tool_choice is invalid")
	}
}

type responsesToolNamespaceContext struct {
	path         []string
	descriptions []string
	execution    string
}

func decodeResponsesTools(tools []responsesToolDefinitionDTO) ([]canonical.ToolDecl, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]canonical.ToolDecl, 0, len(tools))
	seen := map[string]struct{}{}
	for _, tool := range tools {
		decls, err := decodeResponsesToolNode(tool, responsesToolNamespaceContext{})
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
func decodeResponsesToolNode(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext) ([]canonical.ToolDecl, error) {
	kind := strings.ToLower(strings.TrimSpace(tool.Type))
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
		return decodeResponsesNamespaceTool(tool, ctx)
	case "function":
		if len(ctx.path) > 0 {
			decl, err := decodeResponsesNestedFunctionTool(tool, ctx)
			if err != nil {
				return nil, err
			}
			return []canonical.ToolDecl{decl}, nil
		}
		decl, err := decodeResponsesFlatFunctionTool(tool)
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
		decl, err := decodeResponsesFlatCustomTool(tool)
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

func decodeResponsesNamespaceTool(tool responsesToolDefinitionDTO, ctx responsesToolNamespaceContext) ([]canonical.ToolDecl, error) {
	name := strings.TrimSpace(tool.Name) // swobu:io-string source=boundary
	if name == "" {
		return nil, canonical.BadRequest("responses request tool namespace declarations require a name")
	}
	nextCtx := responsesToolNamespaceContext{
		path:         append(append([]string(nil), ctx.path...), name),
		descriptions: append(append([]string(nil), ctx.descriptions...), strings.TrimSpace(tool.Description)),
		execution:    mergeResponsesExecution(ctx.execution, tool.Execution),
	}
	if len(tool.Tools) == 0 {
		return nil, nil
	}
	out := make([]canonical.ToolDecl, 0, len(tool.Tools))
	for _, child := range tool.Tools {
		decls, err := decodeResponsesToolNode(child, nextCtx)
		if err != nil {
			return nil, err
		}
		out = append(out, decls...)
	}
	return out, nil
}

func decodeResponsesFlatFunctionTool(tool responsesToolDefinitionDTO) (canonical.ToolDecl, error) {
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

func decodeResponsesFlatCustomTool(tool responsesToolDefinitionDTO) (canonical.ToolDecl, error) {
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
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if trimmed := strings.TrimSpace(extra); trimmed != "" {
		values = append(values, trimmed)
	}
	return strings.Join(values, "\n\n")
}

func mergeResponsesExecution(parent, child string) string {
	if trimmed := strings.TrimSpace(child); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(parent)
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
		if strings.TrimSpace(tool.OutputFormat) != "" {
			obj["output_format"] = strings.TrimSpace(tool.OutputFormat)
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

func encodeInput(req canonical.CanonicalRequest, forceStructuredInput bool) (any, error) {
	items := req.Items()
	if !req.Turn().IsZero() {
		items = canonical.CurrentTurnDelta(items)
		// Native continuation-only calls should rely on previous_response_id without
		// replaying anchor thread input. Replaying can end with assistant output and
		// violate backend prefill constraints.
		if !hasContinuationDelta(items) { // swobu:io-string source=boundary
			return nil, nil
		}
	}
	if !forceStructuredInput {
		if input, ok, err := encodeSimpleInput(items); ok || err != nil {
			return input, err
		}
	}
	switch len(items) {
	case 0:
		return nil, nil
	default:
		return encodeConversation(items)
	}
}

func encodeSimpleInput(items []canonical.CanonicalItem) (any, bool, error) {
	if len(items) == 0 {
		return nil, false, nil
	}
	if len(items) != 1 {
		return nil, false, nil
	}
	if items[0].Author != "" && items[0].Author != canonical.ItemAuthorUser {
		return nil, false, nil
	}
	text, ok := textOnlyItem(items[0])
	if !ok {
		return nil, false, nil
	}
	return text, true, nil
}

func textOnlyItem(item canonical.CanonicalItem) (string, bool) {
	if item.Kind != canonical.ItemKindText {
		return "", false
	}
	return item.Text, true
}

func hasContinuationDelta(items []canonical.CanonicalItem) bool {
	for _, item := range items {
		if item.Author == canonical.ItemAuthorUser || item.Author == canonical.ItemAuthorTool {
			return true
		}
	}
	return false
}

func encodeConversation(items []canonical.CanonicalItem) ([]any, error) {
	encoded := make([]any, 0, len(items))
	for i := 0; i < len(items); {
		start := i
		current := items[i]
		switch current.Kind {
		case canonical.ItemKindText:
			role := roleForResponsesItem(current)
			var content strings.Builder
			for i < len(items) && items[i].Kind == canonical.ItemKindText && roleForResponsesItem(items[i]) == role {
				content.WriteString(items[i].Text)
				i++
			}
			item := inputMessageItem{
				Type:    "message",
				Role:    role,
				Content: content.String(),
			}
			if role == "assistant" {
				item.ID = sse.FallbackID(current.ItemID, fmt.Sprintf("msg_swobu_%d", start))
				item.Status = "completed"
			}
			encoded = append(encoded, item)
		case canonical.ItemKindToolUse:
			encoded = append(encoded, functionCallItem{
				Type:      "function_call",
				CallID:    current.ToolUseID,
				Name:      current.Name,
				Arguments: current.Input.RawObject(),
			})
			i++
		case canonical.ItemKindToolResult:
			if strings.TrimSpace(current.ToolUseID) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("tool_result items require tool_use_id for the responses protocol")
			}
			encoded = append(encoded, functionCallOutputItem{
				Type:   "function_call_output",
				CallID: current.ToolUseID,
				Output: current.Text,
			})
			i++
		default:
			return nil, canonical.UnsupportedOperation("canonical item is not supported on the responses protocol")
		}
	}
	return encoded, nil
}

func roleForResponsesItem(item canonical.CanonicalItem) string {
	author := item.Author
	if author == canonical.ItemAuthorAssistant {
		return "assistant"
	}
	if author == canonical.ItemAuthorTool {
		return "tool"
	}
	return "user"
}

func cloneBoolPointer(ptr *bool) *bool {
	if ptr == nil {
		return nil
	}
	cloned := *ptr
	return &cloned
}

func normalizeToolExecutionOwner(raw string) canonical.ToolExecutionOwner {
	normalized := strings.ToLower(strings.TrimSpace(raw)) // swobu:io-string source=boundary
	switch canonical.ToolExecutionOwner(normalized) {
	case canonical.ToolOwnerClient, canonical.ToolOwnerProvider, canonical.ToolOwnerSwobu, canonical.ToolOwnerExternal:
		return canonical.ToolExecutionOwner(normalized)
	default:
		return canonical.ToolOwnerClient
	}
}
