package sqlite

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/ledger/ledgertest"
)

func TestStoreConformance(t *testing.T) {
	ledgertest.Run(t, func(tb testing.TB) ledger.Store {
		cfg := testConfig(tb.(*testing.T))
		store, err := Open(context.Background(), cfg)
		if err != nil {
			tb.Fatal(err)
		}
		tb.Cleanup(func() { _ = store.Close() })
		return store
	})
}
