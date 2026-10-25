package bootstrap

import (
	"context"
	"errors"
	"sort"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	domain "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/routing"
)

func (daemon *Daemon) activity(ctx context.Context, scope evidence.ProjectionScope) (evidence.ActivityProjection, error) {
	projection := daemon.executionEvidenceStore.ProjectActivity(evidence.ProjectionInput{Scope: scope})
	if scope.Kind != evidence.ProjectionScopeWorkspace && daemon.ledgerWriter != nil {
		health := daemon.ledgerWriter.Health()
		projection.History = &health
	}
	if daemon.ledgerOpenFailed {
		return projection, errors.New("durable Activity unavailable")
	}
	if daemon.ledgerStore == nil {
		return projection, nil
	}
	var workspace routing.WorkspaceSlug
	if scope.Kind == evidence.ProjectionScopeWorkspace {
		var err error
		workspace, err = routing.ParseWorkspaceSlug(scope.Workspace)
		if err != nil {
			return projection, err
		}
	}
	operations, err := daemon.ledgerStore.ListMCPOperations(ctx, workspace, 1000)
	if err != nil {
		return projection, err
	}
	filter := ledger.ExchangeFilter{}
	if scope.Kind == evidence.ProjectionScopeWorkspace {
		filter.Workspace = &workspace
	}
	page, err := daemon.ledgerStore.ListRecentSummaries(ctx, filter, ledger.Cursor{}, ledger.MaximumPageSize)
	if err != nil {
		return projection, err
	}
	history := make([]domain.CompletedExchange, 0, len(page.Exchanges))
	for _, summary := range page.Exchanges {
		completed, found, err := daemon.ledgerStore.GetExchange(ctx, summary.ID)
		if err != nil {
			return projection, err
		}
		if found {
			history = append(history, completed)
		}
	}
	merged := daemon.executionEvidenceStore.ProjectActivity(evidence.ProjectionInput{Scope: scope, History: history})
	projection.Exchanges = merged.Exchanges
	seen := make(map[string]bool, len(projection.MCPOperations))
	for _, operation := range projection.MCPOperations {
		seen[operation.OperationID] = true
	}
	for _, operation := range operations {
		if seen[operation.ID()] {
			continue
		}
		projection.MCPOperations = append(projection.MCPOperations, evidence.MCPOperation{OperationID: operation.ID(), AccessID: operation.AccessID().String(), Workspace: operation.Workspace().String(), Source: operation.Source(), Tool: operation.Tool(), StartedAt: operation.StartedAt(), SettledAt: operation.SettledAt(), Outcome: operation.Outcome(), Occurrence: operation.Occurrence()})
	}
	sort.Slice(projection.MCPOperations, func(first, second int) bool {
		left, right := projection.MCPOperations[first], projection.MCPOperations[second]
		if left.StartedAt.Equal(right.StartedAt) {
			return left.OperationID > right.OperationID
		}
		return left.StartedAt.After(right.StartedAt)
	})
	return projection, nil
}
