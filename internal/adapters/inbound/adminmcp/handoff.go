package adminmcp

import (
	"context"
	"errors"
	"net/url"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	connectionapp "github.com/swobuforge/swobu/internal/app/operator/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/mcp"
)

// LocalHandoff points to an existing authenticated Operator surface. It is
// neither a credential, a completion acknowledgement nor a new approval/session.
// The human's host keeps one-shot material; the agent verifies metadata later.
type LocalHandoff struct {
	Status     string `json:"status"`
	Path       string `json:"local_operator_path"`
	Action     string `json:"action"`
	VerifyWith string `json:"verify_with"`
}

func SecretHandoffs(access accessapp.Service, connections *connectionapp.Queries) []mcp.AdminTool {
	return []mcp.AdminTool{
		typedTool("credential_issue_handoff", "Arrange one-shot credential issuance through the authenticated local Web Operator. This tool does not issue or receive a bearer. Ask the host operator to complete the indicated action, then verify the new credential metadata.", objectSchema(`"principal":{"type":"string"},"workspace":{"type":"string"}`, "principal", "workspace"), func(ctx context.Context, _ authority, input struct {
			Principal string `json:"principal"`
			Workspace string `json:"workspace"`
		}) (any, error) {
			if _, err := access.GetPrincipal(ctx, input.Principal); err != nil {
				return nil, err
			}
			permission, err := access.ReadPermission(input.Principal, input.Workspace)
			if err != nil {
				return nil, err
			}
			if !permission.Allowed {
				return nil, errors.New("grant the intended Workspace permission before issuing")
			}
			return LocalHandoff{Status: "requires_local_operator", Path: "/access/" + url.PathEscape(input.Principal), Action: "Issue a credential for Workspace " + input.Workspace + ". Deliver the one-shot bearer directly to the intended client; keep it out of the agent conversation.", VerifyWith: "principal_credentials_list"}, nil
		}),
		typedTool("connection_credentials_handoff", "Arrange provider credential entry/rotation in the authenticated local Web Operator. Secret material never enters tool arguments or results. Verify public metadata and run a Connection check after the operator completes it.", objectSchema(`"id":{"type":"string"}`, "id"), func(ctx context.Context, _ authority, input struct {
			ID string `json:"id"`
		}) (any, error) {
			id, err := connectionidentity.Parse(input.ID)
			if err != nil {
				return nil, err
			}
			if _, err := connections.Get(ctx, id); err != nil {
				return nil, err
			}
			return LocalHandoff{Status: "requires_local_operator", Path: "/connections/" + url.PathEscape(input.ID), Action: "Complete credential entry or rotation on the Node host using its supported credential seam. Keep all material outside the agent conversation.", VerifyWith: "connection_get, connection_check"}, nil
		}),
	}
}
