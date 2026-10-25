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
		previousBytes, previousDetail := after, detail
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
		if after >= previousBytes && detail >= previousDetail {
			t.Fatalf("active pressure cycle stalled between watermarks: bytes=%d maximum=%d low=%d detail=%d", after, maximum, maximum*85/100, detail)
		}
	}
	if after > uint64(maximum*85/100) && detail > 0 {
		t.Fatalf("bounded maintenance did not converge: bytes=%d target=%d detail=%d", after, maximum*85/100, detail)
	}
	if db.bytePressure {
		t.Fatal("completed pressure cycle remained active")
	}
}

func TestByteBudgetDoesNotStartPressureBelowHighWatermark(t *testing.T) {
	ctx := t.Context()
	db, err := Open(ctx, testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.WriteBatch(ctx, []executionevidence.CompletedExchange{completedWithoutInvocations(t, time.Now().UTC(), delivery.Succeeded)}); err != nil {
		t.Fatal(err)
	}
	current, err := db.allocatedBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	maximum := int64(current + 4096)
	db.retention.SQLiteMaxBytes = &maximum
	if err := db.enforceByteBudget(ctx); err != nil {
		t.Fatal(err)
	}
	var detail int
	if err := db.writer.QueryRowContext(ctx, `SELECT count(*) FROM swobu_ledger_exchanges`).Scan(&detail); err != nil {
		t.Fatal(err)
	}
	if detail != 1 || db.bytePressure {
		t.Fatal("fresh below-high budget evicted history or activated pressure")
	}
}
