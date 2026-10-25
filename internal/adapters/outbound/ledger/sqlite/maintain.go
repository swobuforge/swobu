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
	if _, err := db.deleteChunk(ctx, `DELETE FROM swobu_ledger_mcp_operations WHERE operation_id IN (
SELECT operation_id FROM swobu_ledger_mcp_operations WHERE settled_at_ns < ? ORDER BY settled_at_ns, operation_id LIMIT ?
)`, detailBefore); err != nil {
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
	if err := db.vacuumFreelist(ctx); err != nil {
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
	if current > maximum {
		db.bytePressure = true
	}
	if !db.bytePressure {
		return nil
	}
	low := maximum * 85 / 100
	for chunks := 0; current > low && chunks < byteBudgetChunksPerCycle; chunks++ {
		deleted, err := db.deleteOldestDetails(ctx)
		if err != nil {
			return err
		}
		if deleted == 0 {
			db.bytePressure = false
			break
		}
		current, err = db.reclaimAndMeasure(ctx)
		if err != nil {
			return err
		}
	}
	if current <= low {
		db.bytePressure = false
	}
	return nil
}

func (db *DB) deleteOldestDetails(ctx context.Context) (uint64, error) {
	tx, err := db.writer.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT kind, identity FROM (
SELECT 'exchange' AS kind, exchange_id AS identity, settled_at_ns FROM swobu_ledger_exchanges
UNION ALL SELECT 'mcp', operation_id, settled_at_ns FROM swobu_ledger_mcp_operations
) ORDER BY settled_at_ns, identity, kind LIMIT ?`, retentionChunk)
	if err != nil {
		return 0, err
	}
	type historyKey struct{ kind, identity string }
	keys := make([]historyKey, 0, retentionChunk)
	for rows.Next() {
		var key historyKey
		if err := rows.Scan(&key.kind, &key.identity); err != nil {
			rows.Close()
			return 0, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, key := range keys {
		statement := "DELETE FROM swobu_ledger_exchanges WHERE exchange_id = ?"
		if key.kind == "mcp" {
			statement = "DELETE FROM swobu_ledger_mcp_operations WHERE operation_id = ?"
		}
		if _, err := tx.ExecContext(ctx, statement, key.identity); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return uint64(len(keys)), nil
}

func (db *DB) reclaimAndMeasure(ctx context.Context) (uint64, error) {
	if err := db.vacuumFreelist(ctx); err != nil {
		return 0, err
	}
	if _, err := db.writer.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
		return 0, err
	}
	return db.allocatedBytes(ctx)
}

func (db *DB) vacuumFreelist(ctx context.Context) error {
	if _, err := db.writer.ExecContext(ctx, `PRAGMA incremental_vacuum`); err != nil {
		return err
	}
	return nil
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
