package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	connectionapp "github.com/swobuforge/swobu/internal/app/operator/connections"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/profile"
)

// ConnectionAuthoringTools permits explicit credential-free network specs.
// Provider credentials and stdio launch configuration use the existing host
// authoring surface, rather than accepting material or shell input from a model.
func ConnectionAuthoringTools(commands *connectionapp.Commands) []mcp.AdminTool {
	return []mcp.AdminTool{
		typedTool("connection_create", "Create a credential-free HTTP MCP, Search or custom Models Connection. Preview with dry_run=true; apply the exact draft with a stable retry_key. For authenticated providers or stdio use connection_create_handoff.", objectSchema(`"display_name":{"type":"string"},"kind":{"type":"string","enum":["mcp","search","model"]},"endpoint":{"type":"string"},"provider_id":{"type":"string"},"execution_identity":{"type":"string"},"permission_summary":{"type":"string"},"retry_key":{"type":"string"},"dry_run":{"type":"boolean"}`, "display_name", "kind", "endpoint", "provider_id", "execution_identity", "permission_summary", "retry_key", "dry_run"), func(ctx context.Context, a authority, input struct {
			DisplayName string `json:"display_name"`
			Kind        string `json:"kind"`
			Endpoint    string `json:"endpoint"`
			Provider    string `json:"provider_id"`
			Identity    string `json:"execution_identity"`
			Permissions string `json:"permission_summary"`
			RetryKey    string `json:"retry_key"`
			DryRun      bool   `json:"dry_run"`
		}) (any, error) {
			var spec any
			switch input.Kind {
			case "mcp":
				if input.Provider != "" {
					return nil, errors.New("MCP has no provider discriminator")
				}
				spec = map[string]any{"transport": "streamable_http", "configuration": map[string]string{"endpoint": input.Endpoint}, "execution_identity": input.Identity, "permission_summary": input.Permissions}
			case "search":
				if input.Identity != "" || input.Permissions != "" {
					return nil, errors.New("unexpected MCP fields")
				}
				spec = map[string]string{"provider_id": input.Provider, "endpoint": input.Endpoint}
			case "model":
				if input.Provider != "custom" || input.Identity != "" || input.Permissions != "" {
					return nil, errors.New("use host authoring for credentialed provider configuration")
				}
				spec = map[string]any{"shape": "custom", "provider_id": "custom", "configuration": map[string]string{"endpoint": input.Endpoint}}
			default:
				return nil, errors.New("invalid kind")
			}
			raw, err := json.Marshal(spec)
			if err != nil {
				return nil, err
			}
			draft := connectionapp.Draft{DisplayName: input.DisplayName, Spec: raw}
			// The closed discriminator is validated again by the canonical constructor.
			draft.Kind = connections.Kind(input.Kind)
			validated, err := draft.ValidatedSpec(profile.SupportsSpec)
			if err != nil {
				return nil, err
			}
			if input.DryRun {
				return struct {
					Valid bool                `json:"valid"`
					Draft connectionapp.Draft `json:"draft"`
				}{true, draft}, nil
			}
			value, replayed, err := commands.Create(ctx, input.DisplayName, validated, "mcp:"+a.principal.String()+":connection.create", input.RetryKey, a.check)
			return struct {
				Connection connectionapp.Connection `json:"connection"`
				Replayed   bool                     `json:"replayed"`
			}{value, replayed}, err
		}),
		typedTool("connection_create_handoff", "Complete authenticated-provider or stdio Connection creation on the Node host. Do not send credentials, credential locators, environment values or launch arguments to the agent. Verify the resulting Connection and probe it after host completion.", objectSchema(""), func(ctx context.Context, _ authority, _ struct{}) (any, error) {
			return LocalHandoff{Status: "requires_local_operator", Path: "/connections", Action: "Create the Connection with the authenticated Web Operator. Enter credential material only through the supported host credential seam.", VerifyWith: "connections_list, connection_get, connection_check"}, nil
		}),
	}
}

func MCPCatalogTools(read func(context.Context, connectionidentity.ID) ([]mcp.CatalogTool, error)) []mcp.AdminTool {
	return []mcp.AdminTool{typedTool("mcp_catalog", "Discover the selected external MCP Connection's catalog and exact revision before mounting tool names. Does not invoke tools or discover unrelated Connections.", objectSchema(`"id":{"type":"string"}`, "id"), func(ctx context.Context, _ authority, input struct {
		ID string `json:"id"`
	}) (any, error) {
		id, err := connectionidentity.Parse(input.ID)
		if err != nil {
			return nil, err
		}
		tools, err := read(ctx, id)
		if err != nil {
			return nil, err
		}
		type item struct {
			Name         string `json:"name"`
			RemoteName   string `json:"remote_name"`
			ConnectionID string `json:"connection_id"`
			Revision     string `json:"connection_revision"`
		}
		out := make([]item, 0, len(tools))
		for _, tool := range tools {
			out = append(out, item{tool.Name, tool.RemoteName, tool.ConnectionID, tool.ConnectionRevision})
		}
		return out, nil
	})}
}

// WorkspaceCatalogTools projects effective mounted names without native
// descriptors or invocation authority. The shared query owns source discovery.
func WorkspaceCatalogTools(read func(context.Context, string) ([]mcp.CatalogTool, error)) []mcp.AdminTool {
	return []mcp.AdminTool{typedTool("workspace_tools_list", "Inspect the effective, explicitly allowlisted Workspace MCP tool names and sources. Does not invoke a tool or grant access.", objectSchema(`"workspace":{"type":"string"}`, "workspace"), func(ctx context.Context, _ authority, input struct {
		Workspace string `json:"workspace"`
	}) (any, error) {
		tools, err := read(ctx, input.Workspace)
		if err != nil {
			return nil, err
		}
		type item struct {
			Name       string `json:"name"`
			Source     string `json:"source"`
			RemoteName string `json:"remote_name"`
		}
		out := make([]item, 0, len(tools))
		for _, tool := range tools {
			out = append(out, item{tool.Name, tool.Source.String(), tool.RemoteName})
		}
		return out, nil
	})}
}
