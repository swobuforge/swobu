package responses

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeRequest_FlattensNamespaceToolsAndResolvesProjectedToolChoice(t *testing.T) {
	t.Parallel()

	functionDecl := canonical.NewFunctionToolDecl(
		"workspace/grep",
		"grep",
		"",
		canonical.NewToolSchemaObject(`{"type":"object","properties":{"pattern":{"type":"string"}}}`),
	)
	projectedFunctionName, err := canonical.ProjectedToolName(functionDecl)
	if err != nil {
		t.Fatalf("ProjectedToolName(function) returned error: %v", err)
	}

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"input":"hi",
		"tool_choice":{"type":"function","name":"` + projectedFunctionName + `"},
		"tools":[
			{
				"name":"workspace",
				"description":"workspace tools",
				"execution":"provider",
				"tools":[
					{
						"type":"function",
						"name":"grep",
						"description":"search text",
						"parameters":{"type":"object","properties":{"pattern":{"type":"string"}}}
					},
					{
						"type":"custom",
						"name":"apply_patch",
						"description":"edit files",
						"execution":"external",
						"format":{"type":"grammar","syntax":"lark","definition":"start: begin_patch hunk+ end_patch"}
					},
					{
						"type":"web_search",
						"name":"search",
						"description":"ignored"
					}
				]
			}
		]
	}`)

	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.WireDocument{
		Family: protocolkind.Responses,
		Raw:    raw,
	})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}

	tools := got.Tools()
	if len(tools) != 2 {
		t.Fatalf("tools len = %d, want 2", len(tools))
	}
	functionTool := requireToolDecl(t, tools, canonical.ToolTypeFunction, "grep")
	customTool := requireToolDecl(t, tools, canonical.ToolTypeCustom, "apply_patch")

	if functionTool.ToolID().String() != canonical.NewSemanticToolIDFor(canonical.ToolOriginRequest, canonical.ToolKindFunction, "workspace/grep").String() {
		t.Fatalf("function tool id = %q, want workspace/grep", functionTool.ToolID())
	}
	if functionTool.ToolDescription() != "workspace tools\n\nsearch text" {
		t.Fatalf("function tool description = %q, want composed description", functionTool.ToolDescription())
	}
	if functionTool.Owner() != canonical.ToolOwnerProvider {
		t.Fatalf("function tool owner = %q, want provider", functionTool.Owner())
	}

	if customTool.ToolID().String() != canonical.NewSemanticToolIDFor(canonical.ToolOriginRequest, canonical.ToolKindCustom, "workspace/apply_patch").String() {
		t.Fatalf("custom tool id = %q, want workspace/apply_patch", customTool.ToolID())
	}
	if customTool.ToolDescription() != "workspace tools\n\nedit files" {
		t.Fatalf("custom tool description = %q, want composed description", customTool.ToolDescription())
	}
	if customTool.Owner() != canonical.ToolOwnerExternal {
		t.Fatalf("custom tool owner = %q, want external", customTool.Owner())
	}

	policy := got.ToolPolicy()
	if policy.Mode != canonical.ToolPolicySpecific {
		t.Fatalf("tool policy mode = %q, want specific", policy.Mode)
	}
	specific, ok := policy.SpecificID()
	if !ok {
		t.Fatal("tool policy specific is missing")
	}
	if specific.String() != functionTool.ToolID().String() {
		t.Fatalf("tool policy specific = %q, want %q", specific, functionTool.ToolID())
	}
	if specificType, ok := policy.SpecificToolType(); !ok || specificType != canonical.ToolTypeFunction {
		t.Fatalf("tool policy specific type = %q, want function", specificType)
	}
}

func TestDecodeRequest_RejectsNamespaceWithoutSupportedChildren(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"input":"hi",
		"tools":[
			{
				"name":"workspace",
				"description":"workspace tools",
				"tools":[
					{
						"type":"web_search",
						"name":"search",
						"description":"ignored"
					}
				]
			}
		]
	}`)

	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.WireDocument{
		Family: protocolkind.Responses,
		Raw:    raw,
	})
	if err == nil {
		t.Fatal("expected DecodeClientRequest to reject a namespace with no supported children")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeUnsupportedOperation {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeUnsupportedOperation)
	}
	if !strings.Contains(compatErr.Message, "no supported child tools") {
		t.Fatalf("error message = %q, want namespace child failure", compatErr.Message)
	}
}

func TestDecodeRequest_DecodesUnnamespacedFlatFunctionToolName(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"input":"ping",
		"tools":[
			{
				"type":"function",
				"name":"Ping",
				"description":"search text",
				"parameters":{"type":"object","properties":{"pattern":{"type":"string"}}}
			}
		]
	}`)

	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.WireDocument{
		Family: protocolkind.Responses,
		Raw:    raw,
	})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	tools := got.Tools()
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	functionTool := requireToolDecl(t, tools, canonical.ToolTypeFunction, "Ping")
	if functionTool.ToolID().String() != canonical.NewSemanticToolIDFor(canonical.ToolOriginRequest, canonical.ToolKindFunction, "Ping").String() {
		t.Fatalf("function tool id = %q, want Ping", functionTool.ToolID())
	}
	if functionTool.ToolDescription() != "search text" {
		t.Fatalf("function tool description = %q, want raw description", functionTool.ToolDescription())
	}
}

func TestDecodeRequest_DecodesLeadingUnderscorePlainFunctionToolNameRaw(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"input":"ping",
		"tool_choice":{"type":"function","name":"__bash__cdaxodhis2"},
		"tools":[
			{
				"type":"function",
				"name":"__bash__cdaxodhis2",
				"description":"search text",
				"parameters":{"type":"object","properties":{"pattern":{"type":"string"}}}
			}
		]
	}`)

	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.WireDocument{
		Family: protocolkind.Responses,
		Raw:    raw,
	})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	tools := got.Tools()
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	functionTool := requireToolDecl(t, tools, canonical.ToolTypeFunction, "__bash__cdaxodhis2")
	wantID := canonical.NewSemanticToolIDFor(canonical.ToolOriginRequest, canonical.ToolKindFunction, "__bash__cdaxodhis2").String()
	if functionTool.ToolID().String() != wantID {
		t.Fatalf("function tool id = %q, want %q", functionTool.ToolID(), wantID)
	}

	policy := got.ToolPolicy()
	if policy.Mode != canonical.ToolPolicySpecific {
		t.Fatalf("tool policy mode = %q, want specific", policy.Mode)
	}
	specific, ok := policy.SpecificID()
	if !ok {
		t.Fatal("tool policy specific is missing")
	}
	if specific.String() != wantID {
		t.Fatalf("tool policy specific = %q, want %q", specific, wantID)
	}
	if specificType, ok := policy.SpecificToolType(); !ok || specificType != canonical.ToolTypeFunction {
		t.Fatalf("tool policy specific type = %q, want function", specificType)
	}
}

func TestDecodeRequest_DecodesProjectedLookingFlatFunctionToolNameAsRaw(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"input":"ping",
		"tool_choice":{"type":"function","name":"exec_command__bogus"},
		"tools":[
			{
				"type":"function",
				"name":"exec_command__bogus",
				"description":"search text",
				"parameters":{"type":"object","properties":{"pattern":{"type":"string"}}}
			}
		]
	}`)

	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.WireDocument{
		Family: protocolkind.Responses,
		Raw:    raw,
	})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	tools := got.Tools()
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	functionTool := requireToolDecl(t, tools, canonical.ToolTypeFunction, "exec_command__bogus")
	wantID := canonical.NewSemanticToolIDFor(canonical.ToolOriginRequest, canonical.ToolKindFunction, "exec_command__bogus").String()
	if functionTool.ToolID().String() != wantID {
		t.Fatalf("function tool id = %q, want %q", functionTool.ToolID(), wantID)
	}

	policy := got.ToolPolicy()
	if policy.Mode != canonical.ToolPolicySpecific {
		t.Fatalf("tool policy mode = %q, want specific", policy.Mode)
	}
	specific, ok := policy.SpecificID()
	if !ok {
		t.Fatal("tool policy specific is missing")
	}
	if specific.String() != wantID {
		t.Fatalf("tool policy specific = %q, want %q", specific, wantID)
	}
	if specificType, ok := policy.SpecificToolType(); !ok || specificType != canonical.ToolTypeFunction {
		t.Fatalf("tool policy specific type = %q, want function", specificType)
	}
}

func requireToolDecl(t *testing.T, tools []canonical.ToolDecl, wantType, wantName string) canonical.ToolDecl {
	t.Helper()
	for _, tool := range tools {
		if canonical.ToolDeclKind(tool) == wantType && tool.ToolName() == wantName {
			return tool
		}
	}
	t.Fatalf("tool %s/%s not found", wantType, wantName)
	return nil
}
