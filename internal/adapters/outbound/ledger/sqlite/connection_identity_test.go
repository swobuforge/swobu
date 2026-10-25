package sqlite

import (
	"context"
	"database/sql"
	"testing"

	modernsqlite "modernc.org/sqlite"
)

func TestConnectionIdentityMigrationFromV2IsTransactional(t *testing.T) {
	cfg := testConfig(t)
	connector, err := modernsqlite.NewConnector(sqliteDSN(cfg.Path))
	if err != nil {
		t.Fatal(err)
	}
	raw := sql.OpenDB(connector)
	defer raw.Close()
	for _, statement := range []string{"PRAGMA auto_vacuum = INCREMENTAL", "PRAGMA journal_mode = WAL", schemaV1, schemaMCPHistory, "INSERT INTO swobu_ledger_meta VALUES(2,'v2-test',1)"} {
		if _, err := raw.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.ExecContext(t.Context(), `INSERT INTO swobu_ledger_exchanges VALUES('historical','local','research',1,2,1,'codex','responses','/v1/responses','succeeded',200,NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.ExecContext(t.Context(), `INSERT INTO swobu_ledger_invocations(invocation_id,exchange_id,ordinal,kind,started_at_ns,settled_at_ns,duration_ns,outcome,execution_state,target_id,target_version_dec,provider_spec,provider_protocol,provider_model,purpose) VALUES('historical-invocation','historical',1,'model',1,2,1,'succeeded','occurred','t','1','openai','responses','m','exchange_attempt')`); err != nil {
		t.Fatal(err)
	}
	conn, err := raw.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := migrateConnectionIdentity(cancelled, conn); err == nil {
		t.Fatal("cancelled migration succeeded")
	}
	var version int
	if err := conn.QueryRowContext(t.Context(), "SELECT schema_version FROM swobu_ledger_meta").Scan(&version); err != nil || version != 2 {
		t.Fatal("failed migration advanced marker")
	}
	if err := migrateConnectionIdentity(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(t.Context(), "SELECT schema_version FROM swobu_ledger_meta").Scan(&version); err != nil || version != 3 {
		t.Fatal("migration failed to advance marker")
	}
	if err := validateConnectionIdentity(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	var absent int
	if err := conn.QueryRowContext(t.Context(), "SELECT count(*) FROM swobu_ledger_invocations WHERE invocation_id='historical-invocation' AND connection_id IS NULL AND connection_revision_dec IS NULL").Scan(&absent); err != nil || absent != 1 {
		t.Fatal("migration fabricated historical binding")
	}
	// A partial pair is rejected by the physical constraint, even outside readers.
	if _, err := conn.ExecContext(t.Context(), `INSERT INTO swobu_ledger_exchanges VALUES('old','local','research',1,2,1,'codex','responses','/v1/responses','succeeded',200,NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(t.Context(), `INSERT INTO swobu_ledger_invocations(invocation_id,exchange_id,ordinal,kind,started_at_ns,settled_at_ns,duration_ns,outcome,execution_state,target_id,target_version_dec,provider_spec,provider_protocol,provider_model,purpose,connection_id) VALUES('i','old',1,'model',1,2,1,'succeeded','occurred','t','1','openai','responses','m','exchange_attempt','cn_a')`); err == nil {
		t.Fatal("partial Connection pair persisted")
	}
}

func TestConnectionIdentityMigrationRollsBackPartialDDL(t *testing.T) {
	cfg := testConfig(t)
	connector, err := modernsqlite.NewConnector(sqliteDSN(cfg.Path))
	if err != nil {
		t.Fatal(err)
	}
	raw := sql.OpenDB(connector)
	defer raw.Close()
	for _, statement := range []string{schemaV1, "INSERT INTO swobu_ledger_meta VALUES(2,'v2-test',1)", "ALTER TABLE swobu_ledger_invocations ADD COLUMN connection_revision_dec TEXT"} {
		if _, err := raw.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	conn, err := raw.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// The second ALTER fails after the first succeeds inside the transaction.
	if err := migrateConnectionIdentity(t.Context(), conn); err == nil {
		t.Fatal("conflicting schema migrated")
	}
	var count, version int
	if err := conn.QueryRowContext(t.Context(), "SELECT count(*) FROM pragma_table_info('swobu_ledger_invocations') WHERE name='connection_id'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed transaction retained first ALTER")
	}
	if err := conn.QueryRowContext(t.Context(), "SELECT schema_version FROM swobu_ledger_meta").Scan(&version); err != nil || version != 2 {
		t.Fatal("failed transaction advanced marker")
	}
}
