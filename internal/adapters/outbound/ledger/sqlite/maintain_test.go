package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
)

func TestMaintainEnforcesSoftByteBudgetByWholeExchange(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	db, err := Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := db.allocatedBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Leave room above the irreducible schema, then create enough history to
	// force pressure-driven whole-lifecycle eviction.
	maximum := int64(baseline + 64*1024)
	cfg.Retention.SQLiteMaxBytes = &maximum
	db, err = Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const exchanges = 1200
	batch := make([]executionevidence.CompletedExchange, 0, 64)
	base := time.Now().UTC().Add(-time.Hour)
	for index := 0; index < exchanges; index++ {
		batch = append(batch, completedWithoutInvocations(t, base.Add(time.Duration(index)*time.Millisecond), delivery.Succeeded))
		if len(batch) == cap(batch) {
			if err := db.WriteBatch(ctx, batch); err != nil {
				t.Fatal(err)
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := db.WriteBatch(ctx, batch); err != nil {
			t.Fatal(err)
		}
	}
	before, err := db.allocatedBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before <= uint64(maximum) {
		t.Fatalf("fixture did not exceed budget: bytes=%d maximum=%d", before, maximum)
	}
	var detailBefore int
	if err := db.writer.QueryRowContext(ctx, `SELECT count(*) FROM swobu_ledger_exchanges`).Scan(&detailBefore); err != nil {
		t.Fatal(err)
	}
	err = db.Maintain(ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var detail, invocations int
	if err := db.writer.QueryRowContext(ctx, `SELECT count(*) FROM swobu_ledger_exchanges`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if err := db.writer.QueryRowContext(ctx, `SELECT count(*) FROM swobu_ledger_invocations`).Scan(&invocations); err != nil {
		t.Fatal(err)
	}
	if invocations != 0 {
		t.Fatalf("zero-Invocation fixture acquired %d orphan Invocation rows", invocations)
	}
	after, err := db.allocatedBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if detail >= detailBefore || after >= before {
		t.Fatalf("bounded maintenance made no progress: before=%d/%d after=%d/%d", before, detailBefore, after, detail)
	}
	for i := 0; i < 32 && after > uint64(maximum*85/100) && detail > 0; i++ {
		if err := db.Maintain(ctx, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		after, err = db.allocatedBytes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.writer.QueryRowContext(ctx, `SELECT count(*) FROM swobu_ledger_exchanges`).Scan(&detail); err != nil {
			t.Fatal(err)
		}
	}
	if after > uint64(maximum*85/100) && detail > 0 {
		t.Fatalf("bounded maintenance did not converge: bytes=%d target=%d detail=%d", after, maximum*85/100, detail)
	}
}
