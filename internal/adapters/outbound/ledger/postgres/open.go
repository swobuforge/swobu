package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/swobuforge/swobu/internal/ledger"
)

const (
	schemaVersion = 1
	lockNamespace = 1398230850
	lockKey       = 1279742513
)

type Config struct {
	DSN              string
	CreatedByVersion string
	Retention        ledger.RetentionConfig
}

type OpenError struct {
	Code  ledger.ErrorCode
	Cause error
}

func (e *OpenError) Error() string { return fmt.Sprintf("PostgreSQL Ledger %s", e.Code) }
func (e *OpenError) Unwrap() error { return e.Cause }

// DB owns one advisory-lock writer session plus a bounded reader pool.
type DB struct {
	writerPool *sql.DB
	writer     *sql.Conn
	readers    *sql.DB
	retention  ledger.RetentionConfig
	writerMu   sync.Mutex
}

var _ ledger.Store = (*DB)(nil)

// Open parses the DSN without projecting it into errors, acquires the fixed M5
// deployment lock, and validates schema before enabling reads or writes.
func Open(ctx context.Context, cfg Config) (*DB, error) {
	if cfg.DSN == "" {
		return nil, pgError(ledger.ErrorInvalidConfig, errors.New("PostgreSQL DSN is required"))
	}
	if err := cfg.Retention.Validate(); err != nil {
		return nil, pgError(ledger.ErrorInvalidConfig, err)
	}
	if cfg.Retention.SQLiteMaxBytes != nil {
		return nil, pgError(ledger.ErrorInvalidConfig, errors.New("PostgreSQL does not support a Ledger byte budget"))
	}
	parsed, err := pgx.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, pgError(ledger.ErrorInvalidConfig, errors.New("PostgreSQL DSN is invalid"))
	}
	writerPool := sql.OpenDB(stdlib.GetConnector(*parsed))
	writerPool.SetMaxOpenConns(1)
	writerPool.SetMaxIdleConns(1)
	readers := sql.OpenDB(stdlib.GetConnector(*parsed))
	readers.SetMaxOpenConns(4)
	readers.SetMaxIdleConns(4)
	db := &DB{writerPool: writerPool, readers: readers, retention: cfg.Retention}
	opened := false
	defer func() {
		if !opened {
			_ = db.Close()
		}
	}()
	db.writer, err = writerPool.Conn(ctx)
	if err != nil {
		return nil, pgError(ledger.ErrorOpenFailed, err)
	}
	var locked bool
	if err := db.writer.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1, $2)`, lockNamespace, lockKey).Scan(&locked); err != nil {
		return nil, pgError(ledger.ErrorOpenFailed, err)
	}
	if !locked {
		return nil, pgError(ledger.ErrorSingleWriterConflict, errors.New("PostgreSQL Ledger database already has an active writer"))
	}
	if err := db.initializeOrValidate(ctx, cfg.CreatedByVersion); err != nil {
		return nil, err
	}
	if err := readers.PingContext(ctx); err != nil {
		return nil, pgError(ledger.ErrorOpenFailed, err)
	}
	opened = true
	return db, nil
}

func (db *DB) initializeOrValidate(ctx context.Context, createdBy string) error {
	var metaExists bool
	if err := db.writer.QueryRowContext(ctx, `SELECT to_regclass('public.swobu_ledger_meta') IS NOT NULL`).Scan(&metaExists); err != nil {
		return pgError(ledger.ErrorOpenFailed, err)
	}
	if !metaExists {
		var userTables int
		if err := db.writer.QueryRowContext(ctx, `SELECT count(*) FROM pg_catalog.pg_tables WHERE schemaname='public'`).Scan(&userTables); err != nil {
			return pgError(ledger.ErrorOpenFailed, err)
		}
		if userTables != 0 {
			return pgError(ledger.ErrorSchemaInvalid, errors.New("unknown non-empty PostgreSQL schema"))
		}
		if err := createSchemaV1(ctx, db.writer, createdBy); err != nil {
			return pgError(ledger.ErrorMigrationFailed, err)
		}
		return nil
	}
	var version int
	if err := db.writer.QueryRowContext(ctx, `SELECT schema_version FROM swobu_ledger_meta`).Scan(&version); err != nil {
		return pgError(ledger.ErrorSchemaInvalid, err)
	}
	if version > schemaVersion {
		return pgError(ledger.ErrorSchemaNewer, fmt.Errorf("unsupported newer schema version %d", version))
	}
	if version != schemaVersion {
		return pgError(ledger.ErrorSchemaInvalid, fmt.Errorf("unsupported schema version %d", version))
	}
	if err := validateSchemaV1(ctx, db.writer); err != nil {
		return pgError(ledger.ErrorSchemaInvalid, err)
	}
	return nil
}

func (db *DB) writerLockHeld(ctx context.Context) error {
	var held bool
	err := db.writer.QueryRowContext(ctx, `SELECT EXISTS (
SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid()
AND classid=$1::bigint::oid AND objid=$2::bigint::oid
)`, lockNamespace, lockKey).Scan(&held)
	if err != nil {
		return err
	}
	if !held {
		return errors.New("PostgreSQL Ledger writer advisory lock was lost")
	}
	return nil
}

// reconnectWriter replaces a dead dedicated session and reacquires the fixed
// deployment lock. PostgreSQL advisory locks are session-owned, so continuing
// to retry a failed *sql.Conn can never recover ownership.
func (db *DB) reconnectWriter(ctx context.Context) error {
	db.writerMu.Lock()
	defer db.writerMu.Unlock()
	if db.writer != nil {
		_ = db.writer.Close()
	}
	conn, err := db.writerPool.Conn(ctx)
	if err != nil {
		return err
	}
	var locked bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1, $2)`, lockNamespace, lockKey).Scan(&locked); err != nil {
		_ = conn.Close()
		return err
	}
	if !locked {
		_ = conn.Close()
		return errors.New("PostgreSQL Ledger writer advisory lock was not reacquired")
	}
	db.writer = conn
	return nil
}

func (db *DB) Close() error {
	if db == nil {
		return nil
	}
	var writerErr error
	if db.writer != nil {
		writerErr = db.writer.Close()
	}
	var writerPoolErr, readerErr error
	if db.writerPool != nil {
		writerPoolErr = db.writerPool.Close()
	}
	if db.readers != nil {
		readerErr = db.readers.Close()
	}
	return errors.Join(writerErr, writerPoolErr, readerErr)
}

func pgError(code ledger.ErrorCode, cause error) error {
	return &OpenError{Code: code, Cause: cause}
}
