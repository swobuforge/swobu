package adminmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/mcp"
)

type authorityKey struct{}
type authority struct {
	principal accessidentity.ID
	check     func(context.Context) error
}

// WithAuthority is called only by admitted Workspace ingress. The callback must
// recheck the original credential, Service, publication and exact mounted source
// at the owning command's effect boundary, without refreshing stale intent.
func WithAuthority(ctx context.Context, principal accessidentity.ID, check func(context.Context) error) context.Context {
	return context.WithValue(ctx, authorityKey{}, authority{principal, check})
}

func caller(ctx context.Context) (authority, error) {
	a, ok := ctx.Value(authorityKey{}).(authority)
	if !ok || !a.principal.Valid() || a.check == nil {
		return authority{}, errors.New("Admin tool requires admitted Workspace MCP authority")
	}
	return a, a.check(ctx)
}

// typedTool closes both schema and argument decoding. Control errors are
// deliberately projected as a constant here: upstream errors may contain secret
// locators or private transport details. Exact intent remains available through
// a fresh read/preview; uncertain applies never acquire an MCP retry ledger.
// A rejected Control command is a tool result, so clients can inspect the
// failure without treating a stale review as a broken MCP protocol.
func typedTool[T any](name, description, schema string, run func(context.Context, authority, T) (any, error)) mcp.AdminTool {
	return mcp.AdminTool{Definition: sdk.Tool{Name: name, Description: description, InputSchema: json.RawMessage(schema)}, Call: func(ctx context.Context, raw json.RawMessage) (*sdk.CallToolResult, error) {
		a, err := caller(ctx)
		if err != nil {
			return nil, err
		}
		var input T
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			return nil, errors.New("invalid Admin tool arguments")
		}
		ctx = accessidentity.WithAttribution(ctx, a.principal)
		value, err := run(ctx, a, input)
		if err != nil {
			return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "Node Control operation failed; read current state before retrying"}}}, nil
		}
		body, err := json.Marshal(value)
		if err != nil {
			return nil, errors.New("Node Control result unavailable")
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(body)}}}, nil
	}}
}

// WorkspaceTools starts with a real query and canonical reviewed creation.
// Preview and apply carry the same complete typed draft. The original Node
// receipt and Principal scope own retry behavior; there is no MCP receipt cache.
func WorkspaceTools(service workspaces.Service) []mcp.AdminTool {
	type workspaceInput struct {
		Workspace string `json:"workspace"`
	}
	type createInput struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"display_name"`
		RetryKey    string `json:"retry_key"`
		DryRun      bool   `json:"dry_run"`
	}
	tools := []mcp.AdminTool{
		typedTool("workspaces_list", "List Node Workspaces and readiness. Contains no credentials.", `{"type":"object","properties":{},"additionalProperties":false}`, func(ctx context.Context, _ authority, _ struct{}) (any, error) {
			return service.ListWorkspaces(ctx)
		}),
		typedTool("workspace_get", "Read a Workspace, its original management ETag and configured Services.", `{"type":"object","properties":{"workspace":{"type":"string"}},"required":["workspace"],"additionalProperties":false}`, func(ctx context.Context, _ authority, input workspaceInput) (any, error) {
			return service.GetWorkspace(ctx, input.Workspace)
		}),
		typedTool("workspace_create", "Preview an empty Workspace with dry_run=true, then apply the exact reviewed slug/display_name with dry_run=false and a stable retry_key. Retry uses the original Node receipt; after uncertainty read current state.", `{"type":"object","properties":{"slug":{"type":"string"},"display_name":{"type":"string"},"retry_key":{"type":"string"},"dry_run":{"type":"boolean"}},"required":["slug","display_name","retry_key","dry_run"],"additionalProperties":false}`, func(ctx context.Context, a authority, input createInput) (any, error) {
			command := workspaces.CreateWorkspace{Slug: input.Slug, DisplayName: input.DisplayName}
			if input.DryRun {
				return service.PreviewCreation(ctx, command, a.check)
			}
			value, replayed, err := service.CreateWorkspaceChecked(ctx, command, "mcp:"+a.principal.String()+":workspace.create", input.RetryKey, a.check)
			return struct {
				Workspace workspaces.Workspace `json:"workspace"`
				Replayed  bool                 `json:"replayed"`
			}{value, replayed}, err
		}),
	}
	return append(tools, workspaceConfigurationTools(service)...)
}
