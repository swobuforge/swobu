package ledger

import (
	"context"
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/routing"
)

type MCPHistoryStore interface {
	WriteMCPOperations(context.Context, []executionevidence.CompletedMCPOperation) error
	ListMCPOperations(context.Context, routing.WorkspaceSlug, int) ([]executionevidence.CompletedMCPOperation, error)
	ListMCPOperationPage(context.Context, routing.WorkspaceSlug, MCPCursor, int) (MCPOperationPage, error)
	GetMCPOperation(context.Context, string) (executionevidence.CompletedMCPOperation, bool, error)
}

// MCPCursor is an exclusive descending settlement-time/operation-ID boundary.
// It is independent of row retention, start time and Workspace authorization.
type MCPCursor struct {
	settledAt time.Time
	operation string
}

func NewMCPCursor(settledAt time.Time, operation string) (MCPCursor, error) {
	if settledAt.IsZero() || operation == "" {
		return MCPCursor{}, errors.New("Ledger MCP cursor requires settlement time and operation identity")
	}
	return MCPCursor{settledAt: settledAt.UTC(), operation: operation}, nil
}

func (cursor MCPCursor) Valid() bool          { return !cursor.settledAt.IsZero() && cursor.operation != "" }
func (cursor MCPCursor) SettledAt() time.Time { return cursor.settledAt }
func (cursor MCPCursor) OperationID() string  { return cursor.operation }

// MCPOperationPage contains retained immutable facts, not live or complete
// capture. Next is present only when another retained row was observed.
type MCPOperationPage struct {
	Operations []executionevidence.CompletedMCPOperation
	Next       MCPCursor
}
