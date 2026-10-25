package adminmcp

import (
	"context"

	"github.com/swobuforge/swobu/internal/accounting"
	limitapp "github.com/swobuforge/swobu/internal/app/operator/limits"
	"github.com/swobuforge/swobu/internal/mcp"
)

func LimitTools(service limitapp.Service) []mcp.AdminTool {
	type workspace struct {
		Workspace string `json:"workspace"`
	}
	type edit struct {
		Workspace string           `json:"workspace"`
		Limit     accounting.Limit `json:"limit"`
		DryRun    bool             `json:"dry_run"`
		ReviewID  string           `json:"review_id"`
	}
	return []mcp.AdminTool{
		typedTool("workspace_limits_status", "Read current Workspace spend policy, coverage and accounting revision. No billable operation is performed.", objectSchema(`"workspace":{"type":"string"}`, "workspace"), func(ctx context.Context, a authority, input workspace) (any, error) {
			return service.Read(ctx, input.Workspace, a.check)
		}),
		typedTool("workspace_limit_set", "Preview exact spend policy with dry_run=true. Apply the same policy with dry_run=false and the original review_id. Review is ordinary delegated command review; it is not independent human approval. After an uncertain outcome read current policy before obtaining a new review.", objectSchema(`"workspace":{"type":"string"},"limit":{"type":"object","properties":{"currency":{"type":"string","enum":["USD"]},"amount_cents":{"type":"integer","minimum":1},"window":{"type":"string","enum":["day","month"]},"mode":{"type":"string","enum":["stop_new_billable_work","observe_only"]},"unknown_price":{"type":"string","enum":["reject"]}},"required":["currency","amount_cents","window","mode","unknown_price"],"additionalProperties":false},"dry_run":{"type":"boolean"},"review_id":{"type":"string"}`, "workspace", "limit", "dry_run", "review_id"), func(ctx context.Context, a authority, input edit) (any, error) {
			if input.DryRun {
				return service.Preview(ctx, input.Workspace, input.Limit, a.check)
			}
			if err := service.Apply(ctx, input.Workspace, input.ReviewID, input.Limit, a.check); err != nil {
				return nil, err
			}
			return service.Read(ctx, input.Workspace, a.check)
		}),
		typedTool("workspace_limit_clear", "Clear spend policy at the original accounting revision, preserving existing reservations and execution evidence.", objectSchema(`"workspace":{"type":"string"},"revision":{"type":"string"}`, "workspace", "revision"), func(ctx context.Context, a authority, input struct {
			Workspace string `json:"workspace"`
			Revision  string `json:"revision"`
		}) (any, error) {
			err := service.Clear(ctx, input.Workspace, input.Revision, a.check)
			return struct {
				Cleared bool `json:"cleared"`
			}{err == nil}, err
		}),
	}
}
