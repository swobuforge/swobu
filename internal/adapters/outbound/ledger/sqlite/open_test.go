package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/swobuforge/swobu/internal/ledger"
	modernsqlite "modernc.org/sqlite"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Path:             filepath.Join(t.TempDir(), "swobu.ledger.sqlite3"),
		CreatedByVersion: "test",
		Retention:        ledger.DefaultSQLiteRetention(),
	}
}

func TestOpenCreatesPrivateStrictWALLedgerAndRestarts(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	db, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range schemaV1Tables {
		var strict int
		if err := db.writer.QueryRowContext(ctx, `SELECT strict FROM pragma_table_list WHERE schema = 'main' AND name = ?`, table).Scan(&strict); err != nil {
			t.Fatalf("table %s: %v", table, err)
		}
		if strict != 1 {
			t.Fatalf("table %s strict = %d, want 1", table, strict)
		}
	}
	if err := verifyConnection(ctx, db.writer); err != nil {
		t.Fatal(err)
	}
	reader, err := db.pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyConnection(ctx, reader); err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	if runtime.GOOS != "windows" {
		info, err := os.Stat(cfg.Path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("database mode = %o, want 600", got)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("ordinary restart: %v", err)
	}
	defer reopened.Close()
	var version int
	if err := reopened.writer.QueryRowContext(ctx, `SELECT schema_version FROM swobu_ledger_meta`).Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("schema after restart = %d, %v", version, err)
	}
}

func modernConnector(path string) (driver.Connector, error) {
	return modernsqlite.NewConnector(sqliteDSN(path))
}
func assertOpenCode(t *testing.T, err error, want ledger.ErrorCode) {
	t.Helper()
	var got *OpenError
	if !errors.As(err, &got) || got.Code != want {
		t.Fatalf("err=%v want code=%s", err, want)
	}
}

func TestOpenRejectsRelativePathBeforeCreatingDatabase(t *testing.T) {
	_, err := Open(context.Background(), Config{Path: "relative.sqlite3", Retention: ledger.DefaultSQLiteRetention()})
	assertOpenCode(t, err, ledger.ErrorInvalidConfig)
	if _, statErr := os.Stat("relative.sqlite3"); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("relative database was created: %v", statErr)
	}
}

func TestOpenPreservesAndRejectsNewerSchema(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	db, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.writer.ExecContext(ctx, `UPDATE swobu_ledger_meta SET schema_version = ?`, schemaVersion+1); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(ctx, cfg)
	assertOpenCode(t, err, ledger.ErrorSchemaNewer)
	connector, err := modernConnector(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw := sql.OpenDB(connector)
	defer raw.Close()
	var version int
	if err := raw.QueryRowContext(ctx, `SELECT schema_version FROM swobu_ledger_meta`).Scan(&version); err != nil || version != schemaVersion+1 {
		t.Fatalf("newer schema was not preserved: version=%d err=%v", version, err)
	}
}

func TestOpenPreservesAndRejectsUnknownNonemptyDatabase(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	connector, err := modernConnector(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw := sql.OpenDB(connector)
	if _, err := raw.ExecContext(ctx, `CREATE TABLE foreign_data(value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = Open(ctx, cfg)
	assertOpenCode(t, err, ledger.ErrorSchemaInvalid)
	connector, err = modernConnector(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	raw = sql.OpenDB(connector)
	defer raw.Close()
	var count int
	if err := raw.QueryRowContext(ctx, `SELECT count(*) FROM foreign_data`).Scan(&count); err != nil {
		t.Fatalf("foreign data was not preserved: %v", err)
	}
}
