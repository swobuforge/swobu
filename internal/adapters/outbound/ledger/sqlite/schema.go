package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

var schemaV1Tables = []string{
	"swobu_ledger_meta",
	"swobu_ledger_exchanges",
	"swobu_ledger_invocations",
}

const schemaV1 = `
CREATE TABLE swobu_ledger_meta (
  schema_version INTEGER NOT NULL PRIMARY KEY,
  created_by_version TEXT NOT NULL,
  created_at_ns INTEGER NOT NULL
) STRICT;
CREATE TABLE swobu_ledger_exchanges (
  exchange_id TEXT PRIMARY KEY,
  access_id TEXT NOT NULL,
  workspace_slug TEXT NOT NULL,
  started_at_ns INTEGER NOT NULL,
  settled_at_ns INTEGER NOT NULL,
  duration_ns INTEGER NOT NULL CHECK(duration_ns >= 0),
  client_family TEXT NOT NULL,
  client_protocol TEXT NOT NULL,
  normalized_operation TEXT NOT NULL,
  delivery_result TEXT NOT NULL,
  client_status_code INTEGER,
  canonical_error_code TEXT
) STRICT;
CREATE TABLE swobu_ledger_invocations (
  invocation_id TEXT PRIMARY KEY,
  exchange_id TEXT NOT NULL REFERENCES swobu_ledger_exchanges(exchange_id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL CHECK(ordinal >= 1),
  kind TEXT NOT NULL CHECK(kind IN ('model', 'mcp_tool')),
  started_at_ns INTEGER NOT NULL,
  settled_at_ns INTEGER NOT NULL,
  duration_ns INTEGER NOT NULL CHECK(duration_ns >= 0),
  outcome TEXT NOT NULL,
  execution_state TEXT NOT NULL,
  failure_class TEXT,
  route_name TEXT,
  target_id TEXT,
  target_version_dec TEXT,
 
  provider_spec TEXT,
  provider_protocol TEXT,
  provider_model TEXT,
  purpose TEXT,
  input_tokens INTEGER CHECK(input_tokens >= 0),
  output_tokens INTEGER CHECK(output_tokens >= 0),
  reasoning_tokens INTEGER CHECK(reasoning_tokens >= 0),
  cache_read_tokens INTEGER CHECK(cache_read_tokens >= 0),
  cache_write_tokens INTEGER CHECK(cache_write_tokens >= 0),
  source_tool_key TEXT,
  tool_key TEXT,
  CHECK((kind = 'model' AND target_id IS NOT NULL AND target_version_dec IS NOT NULL AND provider_spec IS NOT NULL AND provider_protocol IS NOT NULL AND provider_model IS NOT NULL AND purpose IS NOT NULL AND source_tool_key IS NULL AND tool_key IS NULL) OR
        (kind = 'mcp_tool' AND route_name IS NULL AND target_id IS NULL AND target_version_dec IS NULL AND provider_spec IS NULL AND provider_protocol IS NULL AND provider_model IS NULL AND purpose IS NULL AND input_tokens IS NULL AND output_tokens IS NULL AND reasoning_tokens IS NULL AND cache_read_tokens IS NULL AND cache_write_tokens IS NULL AND source_tool_key IS NOT NULL AND tool_key IS NOT NULL)),
  UNIQUE(exchange_id, ordinal)
) STRICT;
CREATE INDEX swobu_ledger_exchanges_settled_idx ON swobu_ledger_exchanges(settled_at_ns DESC, exchange_id DESC);
CREATE INDEX swobu_ledger_exchanges_workspace_idx ON swobu_ledger_exchanges(workspace_slug, settled_at_ns DESC, exchange_id DESC);
CREATE INDEX swobu_ledger_exchanges_access_idx ON swobu_ledger_exchanges(access_id, settled_at_ns DESC, exchange_id DESC);
CREATE INDEX swobu_ledger_invocations_exchange_idx ON swobu_ledger_invocations(exchange_id, ordinal);
CREATE INDEX swobu_ledger_model_provider_idx ON swobu_ledger_invocations(provider_spec, provider_model);
CREATE INDEX swobu_ledger_model_target_idx ON swobu_ledger_invocations(target_id);
CREATE INDEX swobu_ledger_mcp_tool_idx ON swobu_ledger_invocations(tool_key);`

func createSchemaV1(ctx context.Context, conn *sql.Conn, createdBy string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schemaV1); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, schemaMCPHistory); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, schemaConnectionIdentity); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO swobu_ledger_meta(schema_version, created_by_version, created_at_ns) VALUES(?, ?, ?)`, schemaVersion, createdBy, time.Now().UTC().UnixNano()); err != nil {
		return err
	}
	return tx.Commit()
}

func validateSchemaV1(ctx context.Context, conn *sql.Conn) error {
	for _, table := range schemaV1Tables {
		var strict int
		err := conn.QueryRowContext(ctx, `SELECT strict FROM pragma_table_list WHERE schema = 'main' AND name = ?`, table).Scan(&strict)
		if err != nil {
			return fmt.Errorf("required table %s: %w", table, err)
		}
		if strict != 1 {
			return fmt.Errorf("required table %s is not STRICT", table)
		}
	}
	return nil
}
