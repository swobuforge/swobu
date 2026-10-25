package bootstrap

import (
	"context"
	"errors"
	"strings"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/execution"
)

func (daemon *Daemon) activityDetail(ctx context.Context, identity string) (evidence.OperationDetail, bool, error) {
	if strings.HasPrefix(identity, "ex_") {
		id, err := execution.ParseExchangeID(identity)
		if err != nil {
			return evidence.OperationDetail{}, false, err
		}
		if record, found := daemon.executionEvidenceStore.Exchange(id); found {
			return evidence.ProjectExchangeDetail(record, "memory"), true, nil
		}
		if daemon.ledgerOpenFailed {
			return evidence.OperationDetail{}, false, errors.New("retained Activity unavailable")
		}
		if daemon.ledgerStore == nil {
			return evidence.OperationDetail{}, false, nil
		}
		completed, found, err := daemon.ledgerStore.GetExchange(ctx, id)
		if err != nil || !found {
			return evidence.OperationDetail{}, found, err
		}
		settled := completed.Settled()
		record := evidence.ExchangeLifecycle{Started: completed.Started(), Settled: &settled}
		for _, invocation := range completed.Invocations() {
			record.Invocations = append(record.Invocations, evidence.InvocationLifecycle{Started: invocation.Started(), Settled: invocation.Settled()})
		}
		return evidence.ProjectExchangeDetail(record, "retained"), true, nil
	}
	if operation, found := daemon.executionEvidenceStore.MCPOperation(identity); found {
		return evidence.OperationDetail{Kind: "mcp_operation", Evidence: "memory", MCP: &operation}, true, nil
	}
	if daemon.ledgerOpenFailed {
		return evidence.OperationDetail{}, false, errors.New("retained Activity unavailable")
	}
	if daemon.ledgerStore == nil {
		return evidence.OperationDetail{}, false, nil
	}
	operation, found, err := daemon.ledgerStore.GetMCPOperation(ctx, identity)
	if err != nil || !found {
		return evidence.OperationDetail{}, found, err
	}
	row := evidence.MCPOperation{OperationID: operation.ID(), AccessID: operation.AccessID().String(), Workspace: operation.Workspace().String(), Source: operation.Source(), Tool: operation.Tool(), StartedAt: operation.StartedAt(), SettledAt: operation.SettledAt(), Outcome: operation.Outcome(), Occurrence: operation.Occurrence()}
	return evidence.OperationDetail{Kind: "mcp_operation", Evidence: "retained", MCP: &row}, true, nil
}
