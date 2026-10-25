package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNativeRestorationDoesNotPromoteWrongCaseProtocolFields(t *testing.T) {
	listed := &sdk.ListToolsResult{Tools: []*sdk.Tool{{Name: "fetch", InputSchema: json.RawMessage(`{"type":"object"}`)}}}
	raw := json.RawMessage(`{"tools":[{"name":"fetch","Name":"forged","inputSchema":{"type":"object"},"InputSchema":{"type":"string"},"_meta":{"integer":9007199254740993},"_META":{"integer":0}}]}`)
	if err := restoreNativeResponse(listed, raw); err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "fetch" {
		t.Fatal("restoration promoted wrong-case tool identity")
	}
	encoded, err := json.Marshal(listed.Tools[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"type":"object"`) || !strings.Contains(string(encoded), "9007199254740993") {
		t.Fatalf("restoration changed exact schema/metadata: %s", encoded)
	}
	result := &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "done"}}}
	raw = json.RawMessage(`{"content":[{"type":"text","text":"done","_meta":{"integer":9007199254740993},"_META":{"integer":0}}],"structuredContent":{"integer":9007199254740993},"StructuredContent":{"integer":0},"_meta":{"integer":9007199254740993},"_META":{"integer":0}}`)
	if err := restoreNativeResponse(result, raw); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"structured": result.StructuredContent, "metadata": result.Meta, "content": result.Content[0]} {
		encoded, err := json.Marshal(value)
		if err != nil || !strings.Contains(string(encoded), "9007199254740993") {
			t.Fatalf("wrong-case %s replaced protocol truth: %s %v", name, encoded, err)
		}
	}
}
