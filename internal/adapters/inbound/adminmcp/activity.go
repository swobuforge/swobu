package adminmcp

import (
	"context"
	"errors"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/routing"
)

func ActivityTools(read func(context.Context, evidence.ProjectionScope, ledger.ActivityPageQuery) (evidence.ActivityProjection, error), detail func(context.Context, string) (evidence.OperationDetail, bool, error)) []mcp.AdminTool {
	type query struct {
		Workspace      *string `json:"workspace,omitempty"`
		Kind           string  `json:"kind,omitempty"`
		Limit          int     `json:"limit,omitempty"`
		ExchangeCursor string  `json:"exchange_cursor,omitempty"`
		MCPCursor      string  `json:"mcp_cursor,omitempty"`
	}
	return []mcp.AdminTool{
		typedTool("activity_get", "Read content-free current Activity, or retained pages when limit is positive. Node scope includes capture health. Cursors bind Workspace and kind; exhaustion does not prove complete capture or no execution.", objectSchema(`"workspace":{"type":"string","minLength":1},"kind":{"type":"string","enum":["exchange","mcp_operation"]},"limit":{"type":"integer","minimum":1,"maximum":200},"exchange_cursor":{"type":"string","minLength":1},"mcp_cursor":{"type":"string","minLength":1}`), func(ctx context.Context, _ authority, input query) (any, error) {
			scope := evidence.ProjectionScope{Kind: evidence.ProjectionScopeAll}
			workspace := ""
			if input.Workspace != nil {
				workspace = *input.Workspace
				slug, err := routing.ParseWorkspaceSlug(workspace)
				if err != nil || slug.String() != workspace {
					return nil, errors.New("exact Workspace required")
				}
				scope = evidence.ProjectionScope{Kind: evidence.ProjectionScopeWorkspace, Workspace: workspace}
			}
			page, err := ledger.NewActivityPageQuery(workspace, input.Kind, input.Limit, input.ExchangeCursor, input.MCPCursor)
			if err != nil {
				return nil, err
			}
			result, err := read(ctx, scope, page)
			if input.Kind == "exchange" {
				result.MCPOperations = nil
			} else if input.Kind == "mcp_operation" {
				result.Exchanges = []evidence.ActivityExchange{}
			}
			return result, err
		}),
		typedTool("activity_operation_get", "Read one recorded operation's causal evidence without prompts, tool arguments or response content. Missing evidence does not prove no execution.", objectSchema(`"id":{"type":"string","pattern":"^(ex_|mcp_)[A-Za-z0-9_-]{1,252}$"}`, "id"), func(ctx context.Context, _ authority, input struct {
			ID string `json:"id"`
		}) (any, error) {
			result, found, err := detail(ctx, input.ID)
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, errors.New("operation evidence unavailable")
			}
			return result, nil
		}),
	}
}
