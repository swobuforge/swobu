package responses

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
)

func encodeResponsesTools(tools []canonical.ToolDecl, sink effect.Sink, exchangeID string) ([]any, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]any, 0, len(tools))
	for idx, tool := range tools {
		wire, err := encodeResponsesTool(tool, sink, exchangeID, idx)
		if err != nil {
			return nil, err
		}
		out = append(out, wire)
	}
	return out, nil
}

func encodeResponsesTool(tool canonical.ToolDecl, sink effect.Sink, exchangeID string, index int) (map[string]any, error) {
	if tool == nil {
		return nil, canonical.BadRequest("response request tool declarations are invalid")
	}
	switch decl := tool.(type) {
	case canonical.FunctionToolDecl:
		return encodeResponsesFunctionToolDecl(decl, sink, exchangeID, index)
	case *canonical.FunctionToolDecl:
		return encodeResponsesFunctionToolDecl(*decl, sink, exchangeID, index)
	case canonical.CustomToolDecl:
		return encodeResponsesCustomToolDecl(decl, sink, exchangeID, index)
	case *canonical.CustomToolDecl:
		return encodeResponsesCustomToolDecl(*decl, sink, exchangeID, index)
	case canonical.CapabilityToolDecl:
		return encodeResponsesCapabilityToolDecl(decl)
	case *canonical.CapabilityToolDecl:
		return encodeResponsesCapabilityToolDecl(*decl)
	default:
		return nil, canonical.UnsupportedOperation("responses protocol only supports known tool declaration types")
	}
}

func encodeResponsesFunctionToolDecl(decl canonical.FunctionToolDecl, sink effect.Sink, exchangeID string, index int) (map[string]any, error) {
	name, err := responsesToolWireName(decl)
	if err != nil {
		return nil, err
	}
	if err := emitResponsesToolNameNamespaceDecision(sink, exchangeID, decl, compat.Approx, compat.Subject("wire:/tools/"+strconv.Itoa(index)+"/name")); err != nil {
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

func encodeResponsesCustomToolDecl(decl canonical.CustomToolDecl, sink effect.Sink, exchangeID string, index int) (map[string]any, error) {
	name, err := responsesToolWireName(decl)
	if err != nil {
		return nil, err
	}
	if err := emitResponsesToolNameNamespaceDecision(sink, exchangeID, decl, compat.Approx, compat.Subject("wire:/tools/"+strconv.Itoa(index)+"/name")); err != nil {
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
	if execution := strings.TrimSpace(string(decl.Owner())); execution != "" && execution != "client" { // swobu:io-string source=domain
		wire["execution"] = execution
	}
	if capability == "tool_search" {
		// Preserve the observed tool_search shape even when the owner remains
		// client; the backend still needs the explicit execution hint.
		if _, ok := wire["execution"]; !ok {
			wire["execution"] = strings.TrimSpace(string(decl.Owner())) // swobu:io-string source=domain
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

func encodeToolChoice(policy canonical.ToolPolicy, tools []canonical.ToolDecl, sink effect.Sink, exchangeID string) (any, error) {
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
		if err := emitResponsesToolNameNamespaceDecision(sink, exchangeID, decl, compat.Approx, compat.Subject("wire:/tool_choice/name")); err != nil {
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

func emitResponsesToolNameNamespaceDecision(sink effect.Sink, exchangeID string, tool canonical.ToolDecl, outcome compat.Outcome, subject compat.Subject) error {
	if sink == nil || subject == "" {
		return nil
	}
	if tool != nil && !strings.Contains(strings.TrimSpace(tool.ToolID().Path), "/") { // swobu:io-string source=boundary
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, []effect.Effect{
		effect.CompatibilityEffect{
			Feature: compat.ToolNameNamespace,
			Outcome: outcome,
			Subject: subject,
		},
	}); err != nil {
		return canonical.InternalError("compatibility effect sink commit failed")
	}
	return nil
}

type responsesToolNamespaceContext struct {
	path         []string
	descriptions []string
	execution    string
	index        int
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
