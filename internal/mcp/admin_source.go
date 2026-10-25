package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/routing"
)

// AdminTool is a daemon-composed typed adapter, never a user-authored source.
// Call must retain ingress admission and use the canonical Control command.
type AdminTool struct {
	Definition sdk.Tool
	Call       func(context.Context, json.RawMessage) (*sdk.CallToolResult, error)
}

// NewAdminSourceBinding builds the sole closed in-process source variant. The
// private marker cannot be selected by an external Connection or source label.
// This is a binding in the ordinary Workspace catalog, not another MCP server.
func NewAdminSourceBinding(tools []AdminTool) (SourceBinding, error) {
	declarations := make([]canonical.ToolDeclaration, 0, len(tools))
	native := make(map[string]json.RawMessage, len(tools))
	calls := make(map[string]func(context.Context, json.RawMessage) (*sdk.CallToolResult, error), len(tools))
	for _, tool := range tools {
		if tool.Definition.Name == "" || tool.Call == nil || calls[tool.Definition.Name] != nil {
			return SourceBinding{}, fmt.Errorf("invalid builtin Admin tool")
		}
		encoded, err := json.Marshal(tool.Definition.InputSchema)
		if err != nil {
			return SourceBinding{}, err
		}
		object, err := canonical.ParseJSONObject(encoded)
		if err != nil {
			return SourceBinding{}, err
		}
		key, err := canonical.NewToolKey("mcp/swobu-admin", canonical.ToolKindFunction, tool.Definition.Name)
		if err != nil {
			return SourceBinding{}, err
		}
		declaration, err := canonical.NewFunctionTool(key, tool.Definition.Description, canonical.NewToolSchemaObject(object), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
		if err != nil {
			return SourceBinding{}, err
		}
		declarations = append(declarations, declaration)
		native[tool.Definition.Name], err = freezeNativeTool(&tool.Definition)
		if err != nil {
			return SourceBinding{}, err
		}
		calls[tool.Definition.Name] = tool.Call
	}
	key, err := canonical.NewToolKey("mcp", canonical.ToolKindMCP, routing.AdminMCPSource)
	if err != nil {
		return SourceBinding{}, err
	}
	declaration, err := canonical.NewMCPToolSource(key, "Swobu Node administration", canonical.MCPSource{}, declarations)
	if err != nil {
		return SourceBinding{}, err
	}
	source, _ := declaration.MCP()
	return SourceBinding{ID: "builtin:" + routing.AdminMCPSource, builtinAdmin: true, Source: source, NativeTools: native,
		Call: func(ctx context.Context, name string, args json.RawMessage) (*sdk.CallToolResult, error) {
			call := calls[name]
			if call == nil {
				return nil, fmt.Errorf("unknown Admin tool")
			}
			return call(ctx, args)
		}}, nil
}
