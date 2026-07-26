package messages

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/testkit/providertest"
)

func TestClientRequestDecoder_DecodesExplicitToolUseAndToolResultID(t *testing.T) {
	t.Parallel()

	functionTool := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "workspace/Read"), "read files", canonicaltest.Schema(t, `{"type":"object","properties":{"path":{"type":"string"}}}`), canonical.Unspecified[bool]())
	projectedFunctionName := providertest.ProjectedToolName(t, functionTool)
	raw := []byte(`{
		"model":"m",
		"tools":[
			{
				"name":"` + projectedFunctionName + `",
				"description":"read files",
				"input_schema":{"type":"object","properties":{"path":{"type":"string"}}}
			}
		],
		"messages":[
			{"role":"assistant","content":[
				{"type":"text","text":"working"},
				{"type":"tool_use","id":"toolu_swobu_0_1","name":"` + projectedFunctionName + `","input":{"path":"workspace/file.txt"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"toolu_swobu_0_1","content":"Hello, World!"}
			]}
		]
	}`)

	request, clientDelivery, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(
		protocolkind.Messages,
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	))
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	if clientDelivery.Mode != delivery.Buffered {
		t.Fatalf("delivery mode = %s, want buffered", clientDelivery.Mode)
	}

	items := request.Items()
	if len(items) != 4 {
		t.Fatalf("items len = %d, want declarations plus 3 history items", len(items))
	}
	if got := items[2].Kind(); got != canonical.ItemKindToolCall {
		t.Fatalf("tool use kind = %s, want %s", got, canonical.ItemKindToolCall)
	}
	toolUse, _ := items[2].ToolCall()
	if got := toolUse.CallID().String(); got != "toolu_swobu_0_1" {
		t.Fatalf("tool use id = %q, want toolu_swobu_0_1", got)
	}
	if got := items[3].Kind(); got != canonical.ItemKindToolResult {
		t.Fatalf("tool result kind = %s, want %s", got, canonical.ItemKindToolResult)
	}
	toolResult, _ := items[3].ToolResult()
	if got := toolResult.CallID().String(); got != "toolu_swobu_0_1" {
		t.Fatalf("tool result tool_use_id = %q, want toolu_swobu_0_1", got)
	}
	text, _ := toolResult.Content()[0].Text()
	if got := text.Text(); got != "Hello, World!" {
		t.Fatalf("tool result text = %q, want Hello, World!", got)
	}
	tools := canonicaltest.Tools(request)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	if got := tools[0].Key().Name(); got != projectedFunctionName {
		t.Fatalf("tool name = %q, want literal wire identity %q", got, projectedFunctionName)
	}
	function, _ := tools[0].Function()
	if got := function.InputSchema().RawObject(); !strings.Contains(got, `"type":"object"`) || !strings.Contains(got, `"path":{"type":"string"}`) {
		t.Fatalf("tool schema = %q, want schema object", got)
	}
}

func TestClientRequestDecoder_AcceptsHistoricalToolUseWithoutCurrentTools(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"model":"m","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"search","input":{"q":"hello"}}]}]}`)
	request, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: raw})
	if err != nil {
		t.Fatalf("DecodeClientRequest returned error: %v", err)
	}
	call, ok := request.Items()[0].ToolCall()
	if !ok || call.Tool().Namespace() != canonical.ToolNamespaceRequest || call.Tool().Name() != "search" {
		t.Fatalf("historical call tool = %#v, want request/function/search", call.Tool())
	}
}

func TestClientRequestDecoder_RejectsMissingToolUseName(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"m",
		"tools":[
			{
				"name":"Bash",
				"description":"execute shell commands",
				"input_schema":{"type":"object","properties":{"command":{"type":"string"}}}
			}
		],
		"messages":[
			{"role":"assistant","content":[
				{"type":"tool_use","input":{"command":"cat workspace/file.txt"}}
			]}
		]
	}`)

	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(
		protocolkind.Messages,
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	))
	if err == nil {
		t.Fatal("expected DecodeClientRequest to reject missing tool_use name")
	}
}

func TestClientRequestDecoder_RejectsMissingToolResultID(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"model":"m",
		"tools":[
			{
				"name":"Bash",
				"description":"execute shell commands",
				"input_schema":{"type":"object","properties":{"command":{"type":"string"}}}
			}
		],
		"messages":[
			{"role":"assistant","content":[
				{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"first"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","content":"ok"}
			]}
		]
	}`)

	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument(
		protocolkind.Messages,
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	))
	if err == nil {
		t.Fatal("expected DecodeClientRequest to reject missing tool_result tool_use_id")
	}
}
