package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/routing"
)

const schemaMCPHistory = `
CREATE TABLE swobu_ledger_mcp_operations (
 operation_id TEXT PRIMARY KEY,
 access_id TEXT NOT NULL,
 workspace_slug TEXT NOT NULL,
 source_id TEXT NOT NULL,
 tool_name TEXT NOT NULL,
 started_at_ns INTEGER NOT NULL,
 settled_at_ns INTEGER NOT NULL CHECK(settled_at_ns >= started_at_ns),
 outcome TEXT NOT NULL CHECK(outcome IN ('succeeded','failed','cancelled')),
 execution_state TEXT NOT NULL CHECK(execution_state IN ('occurred','may_have_occurred','did_not_occur')),
 CHECK(outcome != 'succeeded' OR execution_state = 'occurred')
) STRICT;
CREATE INDEX swobu_ledger_mcp_operations_workspace_idx ON swobu_ledger_mcp_operations(workspace_slug,settled_at_ns DESC,operation_id DESC);
CREATE INDEX swobu_ledger_mcp_operations_settled_idx ON swobu_ledger_mcp_operations(settled_at_ns,operation_id);`

var _ ledger.MCPHistoryStore = (*DB)(nil)

func migrateMCPHistory(ctx context.Context, conn *sql.Conn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schemaMCPHistory); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE swobu_ledger_meta SET schema_version = 2 WHERE schema_version = 1"); err != nil {
		return err
	}
	return tx.Commit()
}

func validateMCPHistory(ctx context.Context, conn *sql.Conn) error {
	var strict int
	if err := conn.QueryRowContext(ctx, "SELECT strict FROM pragma_table_list WHERE schema = 'main' AND name = 'swobu_ledger_mcp_operations'").Scan(&strict); err != nil {
		return err
	}
	if strict != 1 {
		return errors.New("MCP history table is not STRICT")
	}
	return nil
}

func (db *DB) GetMCPOperation(ctx context.Context, identity string) (executionevidence.CompletedMCPOperation, bool, error) {
	if identity == "" {
		return executionevidence.CompletedMCPOperation{}, false, errors.New("MCP history identity required")
	}
	var id, access, rawWorkspace, source, tool string
	var started, settled int64
	var outcome execution.Outcome
	var occurrence execution.Occurrence
	err := db.pool.QueryRowContext(ctx, `SELECT operation_id,access_id,workspace_slug,source_id,tool_name,started_at_ns,settled_at_ns,outcome,execution_state FROM swobu_ledger_mcp_operations WHERE operation_id = ?`, identity).Scan(&id, &access, &rawWorkspace, &source, &tool, &started, &settled, &outcome, &occurrence)
	if errors.Is(err, sql.ErrNoRows) {
		return executionevidence.CompletedMCPOperation{}, false, nil
	}
	if err != nil {
		return executionevidence.CompletedMCPOperation{}, false, err
	}
	accessID, err := accessidentity.Parse(access)
	if err != nil {
		return executionevidence.CompletedMCPOperation{}, false, err
	}
	workspace, err := routing.ParseWorkspaceSlug(rawWorkspace)
	if err != nil {
		return executionevidence.CompletedMCPOperation{}, false, err
	}
	operation, err := executionevidence.NewCompletedMCPOperation(id, accessID, workspace, source, tool, time.Unix(0, started).UTC(), time.Unix(0, settled).UTC(), outcome, occurrence)
	return operation, err == nil, err
}

func (db *DB) WriteMCPOperations(ctx context.Context, operations []executionevidence.CompletedMCPOperation) error {
	tx, err := db.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, operation := range operations {
		if err := operation.Validate(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO swobu_ledger_mcp_operations(operation_id,access_id,workspace_slug,source_id,tool_name,started_at_ns,settled_at_ns,outcome,execution_state) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(operation_id) DO NOTHING`, operation.ID(), operation.AccessID().String(), operation.Workspace().String(), operation.Source(), operation.Tool(), unixNano(operation.StartedAt()), unixNano(operation.SettledAt()), operation.Outcome(), operation.Occurrence()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) ListMCPOperations(ctx context.Context, workspace routing.WorkspaceSlug, limit int) ([]executionevidence.CompletedMCPOperation, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("MCP history requires limit between 1 and 1000")
	}
	return db.listMCPOperations(ctx, workspace, ledger.MCPCursor{}, limit)
}

func (db *DB) ListMCPOperationPage(ctx context.Context, workspace routing.WorkspaceSlug, cursor ledger.MCPCursor, size int) (ledger.MCPOperationPage, error) {
	limit, err := ledger.NormalizePageSize(size)
	if err != nil {
		return ledger.MCPOperationPage{}, err
	}
	operations, err := db.listMCPOperations(ctx, workspace, cursor, limit+1)
	if err != nil {
		return ledger.MCPOperationPage{}, err
	}
	page := ledger.MCPOperationPage{Operations: operations}
	if len(operations) > limit {
		last := operations[limit-1]
		page.Next, err = ledger.NewMCPCursor(last.SettledAt(), last.ID())
		page.Operations = operations[:limit]
	}
	return page, err
}

func (db *DB) listMCPOperations(ctx context.Context, workspace routing.WorkspaceSlug, cursor ledger.MCPCursor, limit int) ([]executionevidence.CompletedMCPOperation, error) {
	rows, err := db.pool.QueryContext(ctx, `SELECT operation_id,access_id,workspace_slug,source_id,tool_name,started_at_ns,settled_at_ns,outcome,execution_state FROM swobu_ledger_mcp_operations WHERE (? = '' OR workspace_slug = ?) AND (? = '' OR settled_at_ns < ? OR (settled_at_ns = ? AND operation_id < ?)) ORDER BY settled_at_ns DESC,operation_id DESC LIMIT ?`, workspace.String(), workspace.String(), cursor.OperationID(), unixNano(cursor.SettledAt()), unixNano(cursor.SettledAt()), cursor.OperationID(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	operations := make([]executionevidence.CompletedMCPOperation, 0)
	for rows.Next() {
		var id, access, rawWorkspace, source, tool string
		var started, settled int64
		var outcome execution.Outcome
		var occurrence execution.Occurrence
		if err := rows.Scan(&id, &access, &rawWorkspace, &source, &tool, &started, &settled, &outcome, &occurrence); err != nil {
			return nil, err
		}
		accessID, err := accessidentity.Parse(access)
		if err != nil {
			return nil, err
		}
		storedWorkspace, err := routing.ParseWorkspaceSlug(rawWorkspace)
		if err != nil {
			return nil, err
		}
		operation, err := executionevidence.NewCompletedMCPOperation(id, accessID, storedWorkspace, source, tool, time.Unix(0, started).UTC(), time.Unix(0, settled).UTC(), outcome, occurrence)
		if err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, rows.Err()
}
