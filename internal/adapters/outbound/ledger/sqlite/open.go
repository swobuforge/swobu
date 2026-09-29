package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/ledger"
	modernsqlite "modernc.org/sqlite"
)

const (
	schemaVersion    = 1
	journalSizeLimit = 64 * 1024 * 1024
)

// Config contains SQLite-owned open settings. Path must be an absolute local
// filesystem path; callers derive the default from the active config authority.
type Config struct {
	Path             string
	CreatedByVersion string
	Retention        ledger.RetentionConfig
}

// OpenError reports a safe status code while retaining the local cause for
// logs. Callers must not project Cause or the configured path into status.
type OpenError struct {
	Code  ledger.ErrorCode
	Cause error
}

func (e *OpenError) Error() string { return fmt.Sprintf("SQLite Ledger %s: %v", e.Code, e.Cause) }
func (e *OpenError) Unwrap() error { return e.Cause }

// DB owns the bounded pool and its dedicated writer connection. The writer is
// the only connection used for writes, migrations, and retention maintenance.
type DB struct {
	path      string
	pool      *sql.DB
	writer    *sql.Conn
	retention ledger.RetentionConfig
}

var _ ledger.Store = (*DB)(nil)

// Open validates physical SQLite settings and schema before returning a usable
// Ledger. Unknown, malformed, partial, or newer schemas are preserved and
// rejected rather than replaced.
func Open(ctx context.Context, cfg Config) (*DB, error) {
	if !filepath.IsAbs(cfg.Path) {
		return nil, openError(ledger.ErrorInvalidConfig, errors.New("SQLite Ledger path must be absolute"))
	}
	if err := cfg.Retention.Validate(); err != nil || cfg.Retention.SQLiteMaxBytes == nil {
		if err == nil {
			err = errors.New("SQLite Ledger requires a byte retention budget")
		}
		return nil, openError(ledger.ErrorInvalidConfig, err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.Path), 0o700); err != nil {
		return nil, openError(ledger.ErrorOpenFailed, err)
	}
	_, err := createPrivateFile(cfg.Path)
	if err != nil {
		return nil, openError(ledger.ErrorOpenFailed, err)
	}

	connector, err := modernsqlite.NewConnector(sqliteDSN(cfg.Path))
	if err != nil {
		return nil, openError(ledger.ErrorOpenFailed, err)
	}
	pool := sql.OpenDB(connector)
	pool.SetMaxOpenConns(4)
	pool.SetMaxIdleConns(4)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)

	db := &DB{path: cfg.Path, pool: pool, retention: cfg.Retention}
	opened := false
	defer func() {
		if !opened {
			_ = db.Close()
		}
	}()

	db.writer, err = pool.Conn(ctx)
	if err != nil {
		return nil, openError(ledger.ErrorOpenFailed, err)
	}
	if err = db.initializeOrValidate(ctx, cfg.CreatedByVersion); err != nil {
		return nil, err
	}
	if err = verifyConnection(ctx, db.writer); err != nil {
		return nil, openError(ledger.ErrorOpenFailed, fmt.Errorf("verify writer PRAGMAs: %w", err))
	}
	reader, readerErr := pool.Conn(ctx)
	if readerErr != nil {
		return nil, openError(ledger.ErrorOpenFailed, readerErr)
	}
	err = verifyConnection(ctx, reader)
	_ = reader.Close()
	if err != nil {
		return nil, openError(ledger.ErrorOpenFailed, fmt.Errorf("verify reader PRAGMAs: %w", err))
	}
	opened = true
	return db, nil
}

func createPrivateFile(path string) (bool, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err == nil {
		return true, file.Close()
	}
	if !errors.Is(err, os.ErrExist) {
		return false, err
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		return false, statErr
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("SQLite Ledger path is not a regular file")
	}
	return false, nil
}

func sqliteDSN(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := u.Query()
	for _, pragma := range []string{
		"foreign_keys(1)",
		"busy_timeout(5000)",
		"synchronous(NORMAL)",
		"journal_size_limit(" + strconv.Itoa(journalSizeLimit) + ")",
	} {
		query.Add("_pragma", pragma)
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func (db *DB) initializeOrValidate(ctx context.Context, createdBy string) error {
	var tableCount int
	if err := db.writer.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&tableCount); err != nil {
		return openError(ledger.ErrorOpenFailed, err)
	}
	if tableCount == 0 {
		if _, err := db.writer.ExecContext(ctx, `PRAGMA auto_vacuum = INCREMENTAL`); err != nil {
			return openError(ledger.ErrorOpenFailed, err)
		}
		if _, err := db.writer.ExecContext(ctx, `PRAGMA journal_mode = WAL`); err != nil {
			return openError(ledger.ErrorOpenFailed, err)
		}
		if err := createSchemaV1(ctx, db.writer, createdBy); err != nil {
			return openError(ledger.ErrorMigrationFailed, err)
		}
		return nil
	}

	var version int
	err := db.writer.QueryRowContext(ctx, `SELECT schema_version FROM swobu_ledger_meta`).Scan(&version)
	if err != nil {
		return openError(ledger.ErrorSchemaInvalid, err)
	}
	if version > schemaVersion {
		return openError(ledger.ErrorSchemaNewer, fmt.Errorf("schema version %d is newer than supported version %d", version, schemaVersion))
	}
	if version != schemaVersion {
		return openError(ledger.ErrorSchemaInvalid, fmt.Errorf("unsupported schema version %d", version))
	}
	if err := validateSchemaV1(ctx, db.writer); err != nil {
		return openError(ledger.ErrorSchemaInvalid, err)
	}
	return nil
}

func verifyConnection(ctx context.Context, conn *sql.Conn) error {
	wants := []struct {
		name string
		want string
	}{
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
		{"synchronous", "1"},
		{"journal_mode", "wal"},
		{"auto_vacuum", "2"},
		{"journal_size_limit", strconv.Itoa(journalSizeLimit)},
	}
	for _, check := range wants {
		var got string
		if err := conn.QueryRowContext(ctx, `PRAGMA `+check.name).Scan(&got); err != nil {
			return err
		}
		if strings.ToLower(got) != check.want {
			return fmt.Errorf("PRAGMA %s = %q, want %q", check.name, got, check.want)
		}
	}
	return nil
}

// Close releases the dedicated writer before closing the reader pool.
func (db *DB) Close() error {
	if db == nil {
		return nil
	}
	var writerErr error
	if db.writer != nil {
		writerErr = db.writer.Close()
	}
	if db.pool == nil {
		return writerErr
	}
	return errors.Join(writerErr, db.pool.Close())
}

func openError(code ledger.ErrorCode, cause error) error {
	return &OpenError{Code: code, Cause: cause}
}

// DatabasePath exists for adapter-local health and tests. Operator projections
// must never expose this sensitive local path.
func (db *DB) DatabasePath() string { return db.path }

func unixNano(now time.Time) int64 { return now.UTC().UnixNano() }
