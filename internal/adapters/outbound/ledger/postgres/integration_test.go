package postgres

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/ledger/ledgertest"
)

func integrationConfig(t *testing.T) Config {
	t.Helper()
	dsn := os.Getenv("SWOBU_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("SWOBU_TEST_POSTGRES_DSN is not set")
	}
	return Config{DSN: dsn, CreatedByVersion: "test", Retention: ledger.DefaultPostgresRetention()}
}

func TestPostgresStoreConformance(t *testing.T) {
	cfg := integrationConfig(t)
	ledgertest.Run(t, func(tb testing.TB) ledger.Store {
		store, err := Open(context.Background(), cfg)
		if err != nil {
			tb.Fatal(err)
		}
		if _, err := store.writer.ExecContext(context.Background(), `TRUNCATE TABLE swobu_ledger_invocations, swobu_ledger_exchanges, swobu_ledger_mcp_operations`); err != nil {
			tb.Fatal(err)
		}
		tb.Cleanup(func() { _ = store.Close() })
		return store
	})
}

func TestPostgresSingleWriterLockAndSchema(t *testing.T) {
	ctx := context.Background()
	cfg := integrationConfig(t)
	first, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	for _, table := range schemaV1Tables {
		var exists bool
		if err := first.writer.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+table).Scan(&exists); err != nil || !exists {
			t.Fatalf("table %s exists=%v err=%v", table, exists, err)
		}
	}
	second, err := Open(ctx, cfg)
	if second != nil {
		_ = second.Close()
		t.Fatal("second writer acquired the deployment lock")
	}
	var openErr *OpenError
	if !errors.As(err, &openErr) || openErr.Code != ledger.ErrorSingleWriterConflict {
		t.Fatalf("second writer error = %v, want %s", err, ledger.ErrorSingleWriterConflict)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	first = nil
	third, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("writer did not acquire lock after prior close: %v", err)
	}
	defer third.Close()
}

func TestPostgresWriterSessionReconnectsAndReacquiresAdvisoryLock(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, integrationConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.writer.Close(); err != nil {
		t.Fatal(err)
	}
	db.writer = nil
	if err := db.reconnectWriter(ctx); err != nil {
		t.Fatalf("reconnect writer: %v", err)
	}
	if err := db.writerLockHeld(ctx); err != nil {
		t.Fatalf("reacquired writer lock not held: %v", err)
	}
}
