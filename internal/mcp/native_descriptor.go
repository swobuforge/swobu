package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func freezeNativeTool(tool *sdk.Tool) (json.RawMessage, error) {
	encoded, err := json.Marshal(tool)
	if err != nil {
		return nil, err
	}
	if len(encoded) > MaxCatalogBytesPerSource {
		return nil, fmt.Errorf("native MCP descriptor exceeds catalog limit")
	}
	return encoded, nil
}

func publishedNativeTool(entry CatalogTool) (*sdk.Tool, error) {
	var tool sdk.Tool
	if len(entry.NativeDescriptor) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(entry.NativeDescriptor))
		decoder.UseNumber()
		if err := decoder.Decode(&tool); err != nil {
			return nil, err
		}
		if tool.Name != entry.RemoteName {
			return nil, fmt.Errorf("native MCP descriptor identity mismatch")
		}
	} else {
		function, ok := entry.Declaration.Function()
		if !ok {
			return nil, fmt.Errorf("MCP catalog entry is not a function")
		}
		input := json.RawMessage(function.InputSchema().RawObject())
		if len(input) == 0 {
			input = json.RawMessage(`{"type":"object"}`)
		}
		tool = sdk.Tool{Name: entry.RemoteName, Description: function.Description(), InputSchema: input}
	}
	tool.Name = entry.Name
	return &tool, nil
}
