package responses

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestDecodeRequest_PreservesNamespaceToolsAndResolvesLiteralToolChoice(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"gpt-4o-mini",
		"input":"hi",
		"tool_choice":{"type":"function","name":"grep"},
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
					}
				]
			}
		]
	}`)

	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{
		Family: protocolkind.Responses,
		Raw:    raw,
	})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}

	tools := canonicaltest.Tools(got)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want one namespace", len(tools))
	}
	namespace, ok := tools[0].Namespace()
	if !ok || namespace.Description() != "workspace tools" {
		t.Fatalf("namespace = %#v", tools[0])
	}
	children := namespace.Tools()
	if len(children) != 2 {
		t.Fatalf("namespace children = %#v", children)
	}
	functionTool := requireToolDecl(t, children, canonical.ToolTypeFunction, "grep")
	customTool := requireToolDecl(t, children, canonical.ToolTypeCustom, "apply_patch")

	if functionTool.Key().String() != canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "workspace/grep").String() {
		t.Fatalf("function tool id = %q, want workspace/grep", functionTool.Key())
	}
	function, _ := functionTool.Function()
	if function.Description() != "search text" {
		t.Fatalf("function tool description = %q, want source child description", function.Description())
	}

	if customTool.Key().String() != canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "workspace/apply_patch").String() {
		t.Fatalf("custom tool id = %q, want workspace/apply_patch", customTool.Key())
	}
	custom, _ := customTool.Custom()
	if custom.Description() != "edit files" {
		t.Fatalf("custom tool description = %q, want source child description", custom.Description())
	}

	policy := got.ToolPolicy()
	if policy.Mode != canonical.ToolPolicySpecific {
		t.Fatalf("tool policy mode = %q, want specific", policy.Mode)
	}
	specific, ok := policy.SpecificID()
	if !ok {
		t.Fatal("tool policy specific is missing")
	}
	if specific.String() != functionTool.Key().String() {
		t.Fatalf("tool policy specific = %q, want %q", specific, functionTool.Key())
	}
	if specificType := string(specific.Kind()); specificType != canonical.ToolTypeFunction {
		t.Fatalf("tool policy specific type = %q, want function", specificType)
	}
}

func TestDecodeRequest_RejectsNestedWebSearchBecauseBuiltInCannotBeRenamed(t *testing.T) {
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

	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{
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
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
	if !strings.Contains(compatErr.Message, "cannot be nested or renamed") {
		t.Fatalf("error message = %q, want fixed built-in identity failure", compatErr.Message)
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

	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{
		Family: protocolkind.Responses,
		Raw:    raw,
	})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	tools := canonicaltest.Tools(got)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	functionTool := requireToolDecl(t, tools, canonical.ToolTypeFunction, "Ping")
	if functionTool.Key().String() != canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "Ping").String() {
		t.Fatalf("function tool id = %q, want Ping", functionTool.Key())
	}
	function, _ := functionTool.Function()
	if function.Description() != "search text" {
		t.Fatalf("function tool description = %q, want raw description", function.Description())
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

	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{
		Family: protocolkind.Responses,
		Raw:    raw,
	})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	tools := canonicaltest.Tools(got)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	functionTool := requireToolDecl(t, tools, canonical.ToolTypeFunction, "__bash__cdaxodhis2")
	wantID := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "__bash__cdaxodhis2").String()
	if functionTool.Key().String() != wantID {
		t.Fatalf("function tool id = %q, want %q", functionTool.Key(), wantID)
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
	if specificType := string(specific.Kind()); specificType != canonical.ToolTypeFunction {
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

	got, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{
		Family: protocolkind.Responses,
		Raw:    raw,
	})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	tools := canonicaltest.Tools(got)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	functionTool := requireToolDecl(t, tools, canonical.ToolTypeFunction, "exec_command__bogus")
	wantID := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "exec_command__bogus").String()
	if functionTool.Key().String() != wantID {
		t.Fatalf("function tool id = %q, want %q", functionTool.Key(), wantID)
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
	if specificType := string(specific.Kind()); specificType != canonical.ToolTypeFunction {
		t.Fatalf("tool policy specific type = %q, want function", specificType)
	}
}

func requireToolDecl(t *testing.T, tools []canonical.ToolDeclaration, wantType, wantName string) canonical.ToolDeclaration {
	t.Helper()
	for _, tool := range tools {
		if string(tool.Kind()) == wantType && tool.Key().Name() == wantName {
			return tool
		}
	}
	t.Fatalf("tool %s/%s not found", wantType, wantName)
	return canonical.ToolDeclaration{}
}
