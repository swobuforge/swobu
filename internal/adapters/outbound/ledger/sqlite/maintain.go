package sqlite

import (
	"context"
	"fmt"
	"time"
)

const retentionChunk = 256
const byteBudgetChunksPerCycle = 4

// Maintain prunes whole lifecycle detail data in
// bounded transactions. It runs on the dedicated writer; callers schedule it
// from the Ledger writer runtime rather than starting another goroutine here.
func (db *DB) Maintain(ctx context.Context, now time.Time) error {
	detailBefore := now.UTC().AddDate(0, 0, -db.retention.DetailDays).UnixNano()
	_, err := db.deleteChunk(ctx, `DELETE FROM swobu_ledger_exchanges WHERE exchange_id IN (
SELECT exchange_id FROM swobu_ledger_exchanges WHERE settled_at_ns < ? ORDER BY settled_at_ns, exchange_id LIMIT ?
)`, detailBefore)
	if err != nil {
		return err
	}
	if err := db.enforceByteBudget(ctx); err != nil {
		return err
	}
	if _, err := db.writer.ExecContext(ctx, `PRAGMA incremental_vacuum(256)`); err != nil {
		return err
	}
	if _, err := db.writer.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
		return err
	}
	return nil
}

func (db *DB) enforceByteBudget(ctx context.Context) error {
	if _, err := db.writer.ExecContext(ctx, `PRAGMA incremental_vacuum(1024)`); err != nil {
		return err
	}
	if _, err := db.writer.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
		return err
	}
	maximum := uint64(*db.retention.SQLiteMaxBytes)
	current, err := db.allocatedBytes(ctx)
	if err != nil {
		return err
	}
	if current <= maximum {
		return nil
	}
	low := maximum * 85 / 100
	for chunks := 0; current > low && chunks < byteBudgetChunksPerCycle; chunks++ {
		deleted, err := db.deleteOldestDetails(ctx)
		if err != nil {
			return err
		}
		if deleted == 0 {
			break
		}
		current, err = db.reclaimAndMeasure(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) deleteOldestDetails(ctx context.Context) (uint64, error) {
	result, err := db.writer.ExecContext(ctx, `DELETE FROM swobu_ledger_exchanges WHERE exchange_id IN (
SELECT exchange_id FROM swobu_ledger_exchanges ORDER BY settled_at_ns, exchange_id LIMIT ?
)`, retentionChunk)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return uint64(rows), err
}

func (db *DB) reclaimAndMeasure(ctx context.Context) (uint64, error) {
	if _, err := db.writer.ExecContext(ctx, `PRAGMA incremental_vacuum(1024)`); err != nil {
		return 0, err
	}
	if _, err := db.writer.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
		return 0, err
	}
	return db.allocatedBytes(ctx)
}

func (db *DB) allocatedBytes(ctx context.Context) (uint64, error) {
	var pages, pageSize int64
	if err := db.writer.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
		return 0, err
	}
	if err := db.writer.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&pageSize); err != nil {
		return 0, err
	}
	if pages < 0 || pageSize < 0 {
		return 0, fmt.Errorf("SQLite returned invalid physical page accounting")
	}
	return uint64(pages) * uint64(pageSize), nil
}

func (db *DB) deleteChunk(ctx context.Context, statement string, before int64) (uint64, error) {
	result, err := db.writer.ExecContext(ctx, statement, before, retentionChunk)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return uint64(rows), err
}
