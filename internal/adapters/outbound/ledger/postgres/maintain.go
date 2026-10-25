package postgres

import (
	"context"
	"time"
)

const retentionChunk = 256

func (db *DB) Maintain(ctx context.Context, now time.Time) error {
	if err := db.writerLockHeld(ctx); err != nil {
		if reconnectErr := db.reconnectWriter(ctx); reconnectErr != nil {
			return err
		}
		if err := db.writerLockHeld(ctx); err != nil {
			return err
		}
	}
	detailBefore := now.UTC().AddDate(0, 0, -db.retention.DetailDays).UnixNano()
	_, err := db.deleteChunk(ctx, `DELETE FROM swobu_ledger_exchanges WHERE exchange_id IN (SELECT exchange_id FROM swobu_ledger_exchanges WHERE settled_at_ns < $1 ORDER BY settled_at_ns,exchange_id LIMIT $2)`, detailBefore)
	if err != nil {
		return err
	}
	if _, err := db.deleteChunk(ctx, `DELETE FROM swobu_ledger_mcp_operations WHERE operation_id IN (SELECT operation_id FROM swobu_ledger_mcp_operations WHERE settled_at_ns < $1 ORDER BY settled_at_ns,operation_id LIMIT $2)`, detailBefore); err != nil {
		return err
	}
	return nil
}
func (db *DB) deleteChunk(ctx context.Context, statement string, before int64) (uint64, error) {
	result, err := db.writer.ExecContext(ctx, statement, before, retentionChunk)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	return uint64(rows), err
}
