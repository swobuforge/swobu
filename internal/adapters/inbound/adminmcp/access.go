package adminmcp

import (
	"context"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/mcp"
)

// AccessQueries exposes only canonical public Principal and credential metadata.
// Credential material and stored digests are excluded by the application owner.
func AccessQueries(service accessapp.Service) []mcp.AdminTool {
	type principal struct {
		ID string `json:"id"`
	}
	const id = `"id":{"type":"string"}`
	tools := []mcp.AdminTool{
		typedTool("workspace_permission_get", "Read the exact Workspace grant, allowed decision and current review revision for a Principal. Does not issue credentials or change permissions.", objectSchema(`"workspace":{"type":"string"},"principal":{"type":"string"}`, "workspace", "principal"), func(ctx context.Context, _ authority, in struct {
			Workspace string `json:"workspace"`
			Principal string `json:"principal"`
		}) (any, error) {
			return service.ReadPermission(in.Principal, in.Workspace)
		}),

		typedTool("principals_list", "List managed Principals, current permissions and revisions. No credentials are issued.", objectSchema(``), func(ctx context.Context, _ authority, _ struct{}) (any, error) {
			return service.ListPrincipals(ctx), nil
		}),
		typedTool("principal_get", "Read one Principal's current permission scope and revision.", objectSchema(id, "id"), func(ctx context.Context, _ authority, input principal) (any, error) {
			return service.GetPrincipal(ctx, input.ID)
		}),
		typedTool("principal_credentials_list", "List retained credential IDs, Workspace audiences and creation times. Never returns bearer values or digests.", objectSchema(id, "id"), func(ctx context.Context, _ authority, input principal) (any, error) {
			return service.ListCredentials(ctx, input.ID)
		}),
	}
	return append(tools, accessCommands(service)...)
}
