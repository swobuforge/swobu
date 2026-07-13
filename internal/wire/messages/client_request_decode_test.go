package messages

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestClientRequestDecoder_DecodesExplicitToolUseAndToolResultID(t *testing.T) {
	t.Parallel()

	functionTool := canonical.NewFunctionToolDecl("workspace/Read", "Read", "read files", canonical.NewToolSchemaObject(`{"type":"object","properties":{"path":{"type":"string"}}}`))
	projectedFunctionName, err := canonical.ProjectedToolName(functionTool)
	if err != nil {
		t.Fatalf("ProjectedToolName(function) returned error: %v", err)
	}
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
				{"type":"tool_use","id":"toolu_swobu_0_1","name":"Read","input":{"path":"workspace/file.txt"}}
			]},
			{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"toolu_swobu_0_1","content":"Hello, World!"}
			]}
		]
	}`)

	request, clientDelivery, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.NewWireDocument(
		carrier.StageClientRequestIn,
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
	if len(items) != 3 {
		t.Fatalf("items len = %d, want 3", len(items))
	}
	if got := items[1].Kind; got != canonical.ItemKindToolUse {
		t.Fatalf("tool use kind = %s, want %s", got, canonical.ItemKindToolUse)
	}
	if got := items[1].ToolUseID; got != "toolu_swobu_0_1" {
		t.Fatalf("tool use id = %q, want toolu_swobu_0_1", got)
	}
	if got := items[2].Kind; got != canonical.ItemKindToolResult {
		t.Fatalf("tool result kind = %s, want %s", got, canonical.ItemKindToolResult)
	}
	if got := items[2].ToolUseID; got != "toolu_swobu_0_1" {
		t.Fatalf("tool result tool_use_id = %q, want toolu_swobu_0_1", got)
	}
	if got := items[2].Text; got != "Hello, World!" {
		t.Fatalf("tool result text = %q, want Hello, World!", got)
	}
	tools := request.Tools()
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	if got := tools[0].ToolName(); got != "Read" {
		t.Fatalf("tool name = %q, want Read", got)
	}
	if got := tools[0].ToolInputSchema().RawObject(); !strings.Contains(got, `"type":"object"`) || !strings.Contains(got, `"path":{"type":"string"}`) {
		t.Fatalf("tool schema = %q, want schema object", got)
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

	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.NewWireDocument(
		carrier.StageClientRequestIn,
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

	_, _, err := (legacyClientRequestDecoder{}).DecodeClientRequest(carrier.NewWireDocument(
		carrier.StageClientRequestIn,
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
