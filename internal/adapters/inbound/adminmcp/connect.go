package adminmcp

import (
	"context"
	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/routing"
	"net/url"
)

// ConnectTools shares canonical discovery, reviewed planning and credential
// acceptance readback. Host apply keeps filesystem and one-shot bearer custody
// in the existing authenticated Connect workflow.
func ConnectTools(service *connectapp.Service, base string, workspace func(context.Context, routing.WorkspaceSlug) (routing.Workspace, error)) []mcp.AdminTool {
	type input struct {
		Workspace string `json:"workspace"`
		Model     string `json:"model"`
		Principal string `json:"principal"`
		Client    string `json:"client"`
	}
	target := func(ctx context.Context, in input) (clientconnect.Target, error) {
		slug, err := routing.ParseWorkspaceSlug(in.Workspace)
		if err != nil {
			return clientconnect.Target{}, err
		}
		value, err := workspace(ctx, slug)
		if err != nil {
			return clientconnect.Target{}, err
		}
		if in.Model != "" {
			if _, err = value.ResolveRoute(in.Model); err != nil {
				return clientconnect.Target{}, err
			}
		}
		destination, err := clientconnect.NewTarget(in.Workspace, base+"/workspaces/"+url.PathEscape(in.Workspace))
		if err != nil {
			return clientconnect.Target{}, err
		}
		return destination.WithModelID(in.Model).WithAuthentication(in.Principal, ""), nil
	}
	const fields = `"workspace":{"type":"string"},"model":{"type":"string"},"principal":{"type":"string"},"client":{"type":"string"}`
	return []mcp.AdminTool{
		typedTool("clients_list", "Discover supported clients on the Node host for an existing Workspace. Does not inspect or return bearer material.", objectSchema(`"workspace":{"type":"string"}`, "workspace"), func(ctx context.Context, _ authority, in struct {
			Workspace string `json:"workspace"`
		}) (any, error) {
			destination, err := target(ctx, input{Workspace: in.Workspace})
			if err != nil {
				return nil, err
			}
			return service.Discover(ctx, destination)
		}),
		typedTool("client_preview", "Read the exact host client plan and whether its current file and credential are accepted for the selected Principal, Workspace and Models Service. Does not apply changes or return bearer material.", objectSchema(fields, "workspace", "model", "principal", "client"), func(ctx context.Context, _ authority, in input) (any, error) {
			destination, err := target(ctx, in)
			if err != nil {
				return nil, err
			}
			plan, err := service.Plan(ctx, in.Client, destination)
			return projectClientPlan(plan), err
		}),
		typedTool("client_apply_handoff", "Arrange reviewed client configuration on the Node host. The authenticated Connect page owns filesystem writes and one-shot credential custody. After host completion call client_preview and require already_configured=true.", objectSchema(fields, "workspace", "model", "principal", "client"), func(ctx context.Context, _ authority, in input) (any, error) {
			destination, err := target(ctx, in)
			if err != nil {
				return nil, err
			}
			plan, err := service.Plan(ctx, in.Client, destination)
			if err != nil {
				return nil, err
			}
			return struct {
				Plan    clientPlan   `json:"plan"`
				Handoff LocalHandoff `json:"handoff"`
			}{projectClientPlan(plan), LocalHandoff{Status: "requires_local_operator", Path: "/workspaces/" + url.PathEscape(in.Workspace) + "/connect", Action: "Select the reviewed client, model and Principal. Review and apply through authenticated Connect on the Node host. Keep credential material outside the agent conversation.", VerifyWith: "client_preview: already_configured must be true for the same client, Principal, Workspace and model"}}, nil
		}),
	}
}

// Client-owned before/after values are private operator data. Even a field
// named endpoint may contain a credential, so model output carries only
// decisions, field names and the canonical opaque review revision.
type clientPlan struct {
	Revision          string                    `json:"revision"`
	Workspace         string                    `json:"workspace"`
	ModelID           string                    `json:"model_id"`
	Client            connectapp.Client         `json:"client"`
	Authentication    connectapp.Authentication `json:"authentication"`
	AlreadyConfigured bool                      `json:"already_configured"`
	RequiresReplace   bool                      `json:"requires_replace"`
	ChangedFields     []string                  `json:"changed_fields"`
}

func projectClientPlan(plan connectapp.Plan) clientPlan {
	fields := make([]string, 0, len(plan.Changes))
	for _, change := range plan.Changes {
		if !change.BeforeExists || change.Before != change.After {
			fields = append(fields, change.Field)
		}
	}
	return clientPlan{plan.Revision, plan.Workspace, plan.ModelID, plan.Client, plan.Authentication, plan.AlreadyConfigured, plan.RequiresReplace, fields}
}
