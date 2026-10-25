package sqlite

import (
	"context"
	"database/sql"
	"errors"
)

const schemaConnectionIdentity = `
ALTER TABLE swobu_ledger_invocations ADD COLUMN connection_id TEXT;
ALTER TABLE swobu_ledger_invocations ADD COLUMN connection_revision_dec TEXT CHECK((connection_id IS NULL AND connection_revision_dec IS NULL) OR ((kind = 'model' OR kind = 'mcp_tool') AND connection_id IS NOT NULL AND connection_revision_dec IS NOT NULL));`

// migrateConnectionIdentity preserves historical absence instead of guessing a
// Connection from provider labels. Columns and marker advance in one transaction.
func migrateConnectionIdentity(ctx context.Context, conn *sql.Conn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schemaConnectionIdentity); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE swobu_ledger_meta SET schema_version = 3 WHERE schema_version = 2"); err != nil {
		return err
	}

	return tx.Commit()
}

func validateConnectionIdentity(ctx context.Context, conn *sql.Conn) error {
	rows, err := conn.QueryContext(ctx, "SELECT connection_id, connection_revision_dec FROM swobu_ledger_invocations WHERE 1=0")
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	if len(columns) != 2 {
		return errors.New("Connection evidence columns are missing")
	}
	return rows.Err()
}
