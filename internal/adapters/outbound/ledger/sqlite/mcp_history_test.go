package sqlite

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	evidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/routing"
	modernsqlite "modernc.org/sqlite"
)

func TestMCPSettlementCaptureDrainsToDurableHistory(t *testing.T) {
	cfg := testConfig(t)
	db, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := ledger.NewWriter(db, ledger.WriterConfig{})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	store := evidencestore.NewStore(evidencestore.Config{OfferMCP: writer.OfferMCP})
	slug, _ := routing.ParseWorkspaceSlug("research")
	identity, err := store.BeginMCP(accessidentity.Local, slug, "docs", "search")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SettleMCP(identity, execution.Succeeded, execution.Occurred); err != nil {
		t.Fatal(err)
	}
	expected := store.ProjectActivity(evidencestore.ProjectionInput{}).MCPOperations[0]
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	health := writer.Health()
	if health.CommittedMCPOperationsTotal != 1 || health.DroppedMCPOperationsTotal != 0 || health.CommittedExchangesTotal != 0 {
		t.Fatalf("capture health=%#v", health)
	}
	reopened, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	operations, err := reopened.ListMCPOperations(t.Context(), slug, 10)
	if err != nil || len(operations) != 1 {
		t.Fatalf("durable capture=%#v, %v", operations, err)
	}
	operation := operations[0]
	if operation.ID() != expected.OperationID || operation.StartedAt() != expected.StartedAt || operation.SettledAt() != expected.SettledAt || operation.Source() != expected.Source || operation.Tool() != expected.Tool || operation.Outcome() != expected.Outcome || operation.Occurrence() != expected.Occurrence {
		t.Fatalf("settlement capture changed truth: %#v", operation)
	}
}

func TestExchangeSettlementCaptureDrainsToDurableHistory(t *testing.T) {
	cfg := testConfig(t)
	db, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := ledger.NewWriter(db, ledger.WriterConfig{})
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	store := evidencestore.NewStore(evidencestore.Config{OfferExchange: writer.Offer})
	exchange := completedWithoutInvocations(t, time.Now().UTC(), delivery.Succeeded)
	if err := store.Append(exchange.Started()); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(exchange.Settled()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if health := writer.Health(); health.CommittedExchangesTotal != 1 || health.DroppedExchangesTotal != 0 || health.CommittedMCPOperationsTotal != 0 {
		t.Fatalf("Exchange capture health=%#v", health)
	}
	reopened, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	actual, found, err := reopened.GetExchange(t.Context(), exchange.ID())
	if err != nil || !found || actual.Started() != exchange.Started() || actual.Settled() != exchange.Settled() || len(actual.Invocations()) != 0 {
		t.Fatalf("restored Exchange=%#v found=%v err=%v", actual, found, err)
	}
}

func TestMCPOperationsPersistAcrossRestartWithoutExchanges(t *testing.T) {
	cfg := testConfig(t)
	db, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	started := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	operation, err := executionevidence.NewCompletedMCPOperation("mcp_restart", accessidentity.Local, slug, "docs", "search", started, started.Add(time.Second), execution.Failed, execution.MayHaveOccurred)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := db.WriteMCPOperations(t.Context(), []executionevidence.CompletedMCPOperation{operation}); err != nil {
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
	records, err := reopened.ListMCPOperations(t.Context(), slug, 10)
	if err != nil || len(records) != 1 {
		t.Fatalf("history = %#v, %v", records, err)
	}
	if records[0] != operation {
		t.Fatalf("restart changed evidence: %#v", records[0])
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
}

func TestMCPHistoryMigrationPreservesV1Exchanges(t *testing.T) {
	cfg := testConfig(t)
	connector, err := modernsqlite.NewConnector(sqliteDSN(cfg.Path))
	if err != nil {
		t.Fatal(err)
	}
	raw := sql.OpenDB(connector)
	for _, statement := range []string{"PRAGMA auto_vacuum = INCREMENTAL", "PRAGMA journal_mode = WAL", schemaV1, "INSERT INTO swobu_ledger_meta VALUES(1,'v1-test',1)"} {
		if _, err := raw.ExecContext(t.Context(), statement); err != nil {
			raw.Close()
			t.Fatal(err)
		}
	}
	if _, err := raw.ExecContext(t.Context(), `INSERT INTO swobu_ledger_exchanges VALUES('old','local','research',1,2,1,'codex','responses','/v1/responses','succeeded',200,NULL)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, exchanges int
	if err := db.writer.QueryRowContext(t.Context(), "SELECT schema_version FROM swobu_ledger_meta").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("version = %d, %v", version, err)
	}
	if err := db.writer.QueryRowContext(t.Context(), "SELECT count(*) FROM swobu_ledger_exchanges WHERE exchange_id = 'old'").Scan(&exchanges); err != nil || exchanges != 1 {
		t.Fatalf("old Exchanges = %d, %v", exchanges, err)
	}
}

func TestMCPHistoryInvalidBatchRollsBackAndRetentionDeletesOperations(t *testing.T) {
	db, err := Open(t.Context(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	slug, _ := routing.ParseWorkspaceSlug("research")
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	operation, err := executionevidence.NewCompletedMCPOperation("mcp_old", accessidentity.Local, slug, "docs", "search", started, started.Add(time.Second), execution.Succeeded, execution.Occurred)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.WriteMCPOperations(t.Context(), []executionevidence.CompletedMCPOperation{operation, {}}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	records, err := db.ListMCPOperations(t.Context(), slug, 10)
	if err != nil || len(records) != 0 {
		t.Fatalf("partial batch committed: %#v, %v", records, err)
	}
	if err := db.WriteMCPOperations(t.Context(), []executionevidence.CompletedMCPOperation{operation}); err != nil {
		t.Fatal(err)
	}
	if err := db.Maintain(t.Context(), started.AddDate(0, 0, 90)); err != nil {
		t.Fatal(err)
	}
	records, err = db.ListMCPOperations(t.Context(), slug, 10)
	if err != nil || len(records) != 0 {
		t.Fatalf("expired operations retained: %#v, %v", records, err)
	}
}

func TestMCPHistoryBytePruningOrdersBothHistoryKinds(t *testing.T) {
	db, err := Open(t.Context(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	slug, _ := routing.ParseWorkspaceSlug("research")
	started := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	operations := make([]executionevidence.CompletedMCPOperation, 0, retentionChunk)
	for index := range retentionChunk {
		operation, err := executionevidence.NewCompletedMCPOperation(fmt.Sprintf("mcp_%03d", index), accessidentity.Local, slug, "docs", "search", started, started.Add(time.Second), execution.Failed, execution.MayHaveOccurred)
		if err != nil {
			t.Fatal(err)
		}
		operations = append(operations, operation)
	}
	if err := db.WriteMCPOperations(t.Context(), operations); err != nil {
		t.Fatal(err)
	}
	exchange := completedWithoutInvocations(t, started.Add(time.Hour), delivery.Succeeded)
	if err := db.WriteBatch(t.Context(), []executionevidence.CompletedExchange{exchange}); err != nil {
		t.Fatal(err)
	}
	deleted, err := db.deleteOldestDetails(t.Context())
	if err != nil || deleted != retentionChunk {
		t.Fatalf("deleted = %d, %v", deleted, err)
	}
	records, err := db.ListMCPOperations(t.Context(), slug, 1000)
	if err != nil || len(records) != 0 {
		t.Fatalf("older MCP records retained: %d, %v", len(records), err)
	}
	if _, found, err := db.GetExchange(t.Context(), exchange.ID()); err != nil || !found {
		t.Fatalf("newer Exchange removed: found=%v, %v", found, err)
	}
}
