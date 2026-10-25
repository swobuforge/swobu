package adminmcp

import (
	"context"
	"encoding/json"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/routing"
)

// objectSchema assembles only hand-authored closed tool contracts. It does not
// discover Control routes, synthesize commands or widen their arguments.
func objectSchema(properties string, required ...string) string {
	if required == nil {
		required = []string{}
	}
	fields, _ := json.Marshal(required)
	return `{"type":"object","properties":{` + properties + `},"required":` + string(fields) + `,"additionalProperties":false}`
}

func workspaceConfigurationTools(service workspaces.Service) []mcp.AdminTool {
	type named struct {
		Workspace string `json:"workspace"`
	}
	type edit struct {
		Workspace string `json:"workspace"`
		ETag      string `json:"etag"`
	}
	type rename struct {
		edit
		DisplayName string `json:"display_name"`
	}
	type deletion struct {
		edit
		RetryKey string `json:"retry_key"`
	}
	type serviceEdit struct {
		edit
		Service string `json:"service"`
		Enabled bool   `json:"enabled"`
	}
	type route struct {
		Workspace string `json:"workspace"`
		Route     string `json:"route"`
	}
	type routeEdit struct {
		edit
		Route string               `json:"route"`
		Spec  workspaces.RouteSpec `json:"spec"`
	}
	type routeDeletion struct {
		edit
		Route string `json:"route"`
	}
	type order struct {
		edit
		Patterns []string `json:"patterns"`
	}
	type match struct {
		Workspace string `json:"workspace"`
		Model     string `json:"model"`
	}
	type mountEdit struct {
		Workspace     string                            `json:"workspace"`
		Alias         string                            `json:"alias"`
		ConnectionID  string                            `json:"connection_id"`
		Builtin       string                            `json:"builtin"`
		SelectedTools []string                          `json:"selected_tools"`
		DryRun        bool                              `json:"dry_run"`
		Review        *workspaces.MCPMountReviewRequest `json:"review,omitempty"`
	}
	type mountDeletion struct {
		edit
		Alias string `json:"alias"`
	}
	type limitsEdit struct {
		edit
		Limits routing.ServiceLimits `json:"limits"`
	}
	type searchEdit struct {
		edit
		ConnectionID          string   `json:"connection_id"`
		FallbackConnectionIDs []string `json:"fallback_connection_ids"`
	}
	const workspace = `"workspace":{"type":"string"}`
	const etag = `"etag":{"type":"string","minLength":1}`
	const routeName = `"route":{"type":"string"}`
	const quotaSchema = `{"type":"object","properties":{"requests_per_minute":{"type":"integer","minimum":0,"maximum":100000}},"additionalProperties":false}`
	const modelQuotaSchema = `{"type":"object","properties":{"requests_per_minute":{"type":"integer","minimum":0,"maximum":100000},"concurrent":{"type":"integer","minimum":0,"maximum":10000}},"additionalProperties":false}`
	return []mcp.AdminTool{
		typedTool("workspace_service_limits_set", "Set Workspace-wide per-Service request rates and Models concurrency using the original Workspace ETag. Zero means unlimited. Workspace permission grants are unchanged.", objectSchema(workspace+`,`+etag+`,"limits":{"type":"object","properties":{"models":`+modelQuotaSchema+`,"mcp":`+quotaSchema+`,"search":`+quotaSchema+`},"additionalProperties":false}`, "workspace", "etag", "limits"), func(ctx context.Context, a authority, input limitsEdit) (any, error) {
			return service.SetServiceLimitsChecked(ctx, input.Workspace, input.Limits, input.ETag, a.check)
		}),
		typedTool("workspace_rename", "Change display name at the original management ETag. The immutable Workspace slug is preserved.", objectSchema(workspace+`,`+etag+`,"display_name":{"type":"string"}`, "workspace", "etag", "display_name"), func(ctx context.Context, a authority, input rename) (any, error) {
			return service.SetDisplayNameChecked(ctx, input.Workspace, input.DisplayName, input.ETag, a.check)
		}),
		typedTool("workspace_delete", "Delete the reviewed empty, unpublished Workspace after removing its grants and scoped dependencies. Use the original ETag and retry key.", objectSchema(workspace+`,`+etag+`,"retry_key":{"type":"string"}`, "workspace", "etag", "retry_key"), func(ctx context.Context, a authority, input deletion) (any, error) {
			replayed, err := service.DeleteWorkspaceChecked(ctx, input.Workspace, input.ETag, "mcp:"+a.principal.String()+":workspace.delete", input.RetryKey, a.check)
			return struct {
				Replayed bool `json:"replayed"`
			}{replayed}, err
		}),
		typedTool("workspace_services", "Read enabled and configured Services with current activation impact. Disabled Services disclose affected Principals and relay publication. For authority-expanding activation, a trusted local Web Operator must review and confirm the impact.", objectSchema(workspace, "workspace"), func(ctx context.Context, _ authority, input named) (any, error) {
			return service.Services(ctx, input.Workspace)
		}),
		typedTool("workspace_service_set", "Enable a Service only when it does not expand existing Principal grants or published relay reachability; disable an empty Service at the original Workspace ETag. If activation would expand authority, the command is denied and a trusted local Web Operator must review and confirm the impact. Agents cannot self-approve a review digest.", objectSchema(workspace+`,`+etag+`,"service":{"type":"string","enum":["models","mcp","search"]},"enabled":{"type":"boolean"}`, "workspace", "etag", "service", "enabled"), func(ctx context.Context, a authority, input serviceEdit) (any, error) {
			return service.SetServiceChecked(ctx, input.Workspace, input.Service, input.Enabled, input.ETag, a.check)
		}),
		typedTool("model_route_put", "Replace the complete route at its original Workspace ETag. Targets use reusable Connection IDs and explicit protocols; Node owns target versions.", objectSchema(workspace+`,`+etag+`,`+routeName+`,"spec":{"type":"object","properties":{"tiers":{"type":"array","items":{"type":"object","properties":{"targets":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"model":{"type":"string"},"protocol":{"type":"string"},"connection_id":{"type":"string"}},"required":["id","model","protocol","connection_id"],"additionalProperties":false}}},"required":["targets"],"additionalProperties":false}}},"required":["tiers"],"additionalProperties":false}`, "workspace", "etag", "route", "spec"), func(ctx context.Context, a authority, input routeEdit) (any, error) {
			return service.PutModelRouteChecked(ctx, workspaces.ReplaceRoute{Workspace: input.Workspace, Route: input.Route, Spec: input.Spec}, input.ETag, a.check)
		}),
		typedTool("model_route_get", "Read the exact model route draft for editing. Use workspace_get for the original management ETag.", objectSchema(workspace+`,`+routeName, "workspace", "route"), func(ctx context.Context, _ authority, input route) (any, error) {
			return service.GetRoute(ctx, input.Workspace, input.Route)
		}),
		typedTool("model_route_delete", "Remove one model route at its original Workspace ETag.", objectSchema(workspace+`,`+etag+`,`+routeName, "workspace", "etag", "route"), func(ctx context.Context, a authority, input routeDeletion) (any, error) {
			return service.DeleteModelRouteChecked(ctx, input.Workspace, input.Route, input.ETag, a.check)
		}),
		typedTool("model_route_order", "Replace first-match model route order using every current pattern exactly once and the original Workspace ETag.", objectSchema(workspace+`,`+etag+`,"patterns":{"type":"array","items":{"type":"string"}}`, "workspace", "etag", "patterns"), func(ctx context.Context, a authority, input order) (any, error) {
			return service.SetRouteOrderChecked(ctx, input.Workspace, input.Patterns, input.ETag, a.check)
		}),
		typedTool("model_match", "Preview the winning configured model route without executing a model request.", objectSchema(workspace+`,"model":{"type":"string"}`, "workspace", "model"), func(ctx context.Context, _ authority, input match) (any, error) {
			return service.PreviewModelMatch(ctx, input.Workspace, input.Model)
		}),
		typedTool("mcp_mounts", "Read current MCP source variants and exact tool allowlists.", objectSchema(workspace, "workspace"), func(ctx context.Context, _ authority, input named) (any, error) {
			value, err := service.GetWorkspace(ctx, input.Workspace)
			return value.MCPMounts, err
		}),
		typedTool("mcp_mount", "Preview with dry_run=true. Review affected Principals, source identity, and full Node authority for swobu-admin. Apply exact intent with dry_run=false and the original review; do not refresh after an uncertain result.", objectSchema(workspace+`,"alias":{"type":"string"},"connection_id":{"type":"string"},"builtin":{"type":"string","enum":["","swobu-admin"]},"selected_tools":{"type":"array","items":{"type":"string"}},"dry_run":{"type":"boolean"},"review":{"type":"object","properties":{"workspace_etag":{"type":"string"},"digest":{"type":"string"},"connection_revision":{"type":"string"},"access_revision":{"type":"string"},"publication_revision":{"type":"string"}},"required":["workspace_etag","digest","connection_revision","access_revision","publication_revision"],"additionalProperties":false}`, "workspace", "alias", "connection_id", "builtin", "selected_tools", "dry_run"), func(ctx context.Context, a authority, input mountEdit) (any, error) {
			command := workspaces.MCPMountDraft{Workspace: input.Workspace, Alias: input.Alias, ConnectionID: input.ConnectionID, Builtin: input.Builtin, SelectedTools: input.SelectedTools}
			if input.DryRun && input.Review == nil {
				return service.PreviewMCPMount(ctx, command)
			}
			if !input.DryRun && input.Review != nil {
				return service.ApplyMCPMountChecked(ctx, command, *input.Review, a.check)
			}
			return nil, workspaces.CommandError{Code: workspaces.InvalidArgument, Message: "preview excludes review; apply requires the original review"}
		}),
		typedTool("mcp_mount_delete", "Remove a mount at the original Workspace ETag. Removing the calling Admin mount permits this mutation once; subsequent calls lose authority.", objectSchema(workspace+`,`+etag+`,"alias":{"type":"string"}`, "workspace", "etag", "alias"), func(ctx context.Context, a authority, input mountDeletion) (any, error) {
			return service.DeleteMCPMountChecked(ctx, input.Workspace, input.Alias, input.ETag, a.check)
		}),
		typedTool("search_route_get", "Read the primary and ordered fallback Search Connections without executing Search.", objectSchema(workspace, "workspace"), func(ctx context.Context, _ authority, input named) (any, error) {
			value, err := service.GetWorkspace(ctx, input.Workspace)
			return struct {
				ConnectionID          string   `json:"connection_id"`
				FallbackConnectionIDs []string `json:"fallback_connection_ids"`
			}{value.SearchConnectionID, append([]string{}, value.SearchFallbackIDs...)}, err
		}),
		typedTool("search_route_set", "Set the complete ordered Search route at the original Workspace ETag. Empty primary with empty fallbacks clears the route.", objectSchema(workspace+`,`+etag+`,"connection_id":{"type":"string"},"fallback_connection_ids":{"type":"array","items":{"type":"string"},"maxItems":7}`, "workspace", "etag", "connection_id", "fallback_connection_ids"), func(ctx context.Context, a authority, input searchEdit) (any, error) {
			return service.ChangeSearchRouteChecked(ctx, input.Workspace, input.ConnectionID, input.FallbackConnectionIDs, input.ETag, a.check)
		}),
	}
}
