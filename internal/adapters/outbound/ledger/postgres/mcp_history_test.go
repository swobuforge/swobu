package postgres

import (
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestPostgresMCPHistoryRestartIsolationAndRetention(t *testing.T) {
	cfg := integrationConfig(t)
	db, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.writer.ExecContext(t.Context(), "TRUNCATE TABLE swobu_ledger_invocations, swobu_ledger_exchanges, swobu_ledger_mcp_operations"); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	operation, err := executionevidence.NewCompletedMCPOperation("mcp_restart", accessidentity.Local, slug, "docs", "search", started, started.Add(time.Second), execution.Failed, execution.MayHaveOccurred)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WriteMCPOperations(t.Context(), []executionevidence.CompletedMCPOperation{operation, {}}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	records, err := db.ListMCPOperations(t.Context(), slug, 10)
	if err != nil || len(records) != 0 {
		t.Fatalf("partial invalid batch committed: %#v, %v", records, err)
	}
	for range 2 {
		if err := db.WriteMCPOperations(t.Context(), []executionevidence.CompletedMCPOperation{operation}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.WriteMCPOperations(t.Context(), []executionevidence.CompletedMCPOperation{operation}); err != nil {
		t.Fatalf("closed writer did not reacquire ownership: %v", err)
	}
	if err := db.writerLockHeld(t.Context()); err != nil {
		t.Fatalf("MCP write continued without writer ownership: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	records, err = reopened.ListMCPOperations(t.Context(), slug, 10)
	if err != nil || len(records) != 1 || records[0] != operation {
		t.Fatalf("restart history = %#v, %v", records, err)
	}
	all, err := reopened.ListMCPOperations(t.Context(), routing.WorkspaceSlug{}, 10)
	if err != nil || len(all) != 1 || all[0] != operation {
		t.Fatalf("Node-wide history=%#v, %v", all, err)
	}
	other, _ := routing.ParseWorkspaceSlug("other")
	records, err = reopened.ListMCPOperations(t.Context(), other, 10)
	if err != nil || len(records) != 0 {
		t.Fatalf("cross-Workspace history = %#v, %v", records, err)
	}
	var exchanges int
	if err := reopened.writer.QueryRowContext(t.Context(), "SELECT count(*) FROM swobu_ledger_exchanges").Scan(&exchanges); err != nil || exchanges != 0 {
		t.Fatalf("fabricated Exchanges = %d, %v", exchanges, err)
	}
	if err := reopened.Maintain(t.Context(), started.AddDate(0, 0, 90)); err != nil {
		t.Fatal(err)
	}
	records, err = reopened.ListMCPOperations(t.Context(), slug, 10)
	if err != nil || len(records) != 0 {
		t.Fatalf("expired history retained: %#v, %v", records, err)
	}
}

func TestPostgresMCPHistoryMigratesV1PreservingExchange(t *testing.T) {
	cfg := integrationConfig(t)
	db, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		"TRUNCATE TABLE swobu_ledger_invocations, swobu_ledger_exchanges, swobu_ledger_mcp_operations",
		"DROP TABLE swobu_ledger_mcp_operations",
		"ALTER TABLE swobu_ledger_invocations DROP COLUMN connection_revision_dec",
		"ALTER TABLE swobu_ledger_invocations DROP COLUMN connection_id",
		"UPDATE swobu_ledger_meta SET schema_version = 1",
		`INSERT INTO swobu_ledger_exchanges VALUES('v1_exchange','local','research',1,2,1,'codex','responses','/v1/responses','succeeded',200,NULL)`,
	} {
		if _, err := db.writer.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var version, exchanges int
	if err := reopened.writer.QueryRowContext(t.Context(), "SELECT schema_version FROM swobu_ledger_meta").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("schema = %d, %v", version, err)
	}
	if err := reopened.writer.QueryRowContext(t.Context(), "SELECT count(*) FROM swobu_ledger_exchanges WHERE exchange_id = 'v1_exchange'").Scan(&exchanges); err != nil || exchanges != 1 {
		t.Fatalf("preserved Exchanges = %d, %v", exchanges, err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	if _, err := reopened.ListMCPOperations(t.Context(), slug, 10); err != nil {
		t.Fatal(err)
	}
}
