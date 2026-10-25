package bootstrap

import (
	"context"
	"errors"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/routing"
)

func (daemon *Daemon) activityPage(ctx context.Context, scope evidence.ProjectionScope, query ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
	projection := evidence.ActivityProjection{Scope: scope, Exchanges: make([]evidence.ActivityExchange, 0), Page: &ledger.ActivityPage{Limit: query.Limit, Mode: "retained"}}
	if daemon.ledgerOpenFailed || daemon.ledgerStore == nil {
		return projection, errors.New("retained Activity unavailable")
	}
	if scope.Kind != evidence.ProjectionScopeWorkspace && daemon.ledgerWriter != nil {
		health := daemon.ledgerWriter.Health()
		projection.History = &health
	}
	var workspace routing.WorkspaceSlug
	filter := query.ExchangeFilter
	if scope.Kind == evidence.ProjectionScopeWorkspace {
		var err error
		workspace, err = routing.ParseWorkspaceSlug(scope.Workspace)
		if err != nil {
			return projection, err
		}
		filter.Workspace = &workspace
	}
	if query.Kind != "mcp_operation" {
		page, err := daemon.ledgerStore.ListRecentSummaries(ctx, filter, query.Exchange, query.Limit)
		if err != nil {
			return projection, err
		}
		for _, summary := range page.Exchanges {
			completed, found, err := daemon.ledgerStore.GetExchange(ctx, summary.ID)
			if err != nil {
				return projection, err
			}
			if !found {
				return projection, errors.New("retained Activity changed during read")
			}
			store := evidence.NewStore(evidence.Config{})
			row := store.ProjectActivity(evidence.ProjectionInput{Scope: scope, History: []domain.CompletedExchange{completed}})
			projection.Exchanges = append(projection.Exchanges, row.Exchanges...)
		}
		if page.Next.Valid() {
			projection.Page.ExchangeNext = ledger.EncodeActivityCursor(scope.Workspace, "exchange", page.Next.SettledAt(), page.Next.ExchangeID().String())
		}
	}
	if query.Kind != "exchange" {
		page, err := daemon.ledgerStore.ListMCPOperationPage(ctx, workspace, query.MCP, query.Limit)
		if err != nil {
			return projection, err
		}
		for _, operation := range page.Operations {
			projection.MCPOperations = append(projection.MCPOperations, evidence.MCPOperation{OperationID: operation.ID(), AccessID: operation.AccessID().String(), Workspace: operation.Workspace().String(), Source: operation.Source(), Tool: operation.Tool(), StartedAt: operation.StartedAt(), SettledAt: operation.SettledAt(), Outcome: operation.Outcome(), Occurrence: operation.Occurrence()})
		}
		if page.Next.Valid() {
			projection.Page.MCPNext = ledger.EncodeActivityCursor(scope.Workspace, "mcp_operation", page.Next.SettledAt(), page.Next.OperationID())
		}
	}
	return projection, nil
}
