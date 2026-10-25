package ledger

import (
	"context"
	"time"

	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
)

// Store is the synchronous, portable Ledger contract implemented by SQLite
// and PostgreSQL. WriteBatch atomically commits first-seen completed lifecycles,
// Retrying an already committed Exchange is a no-op.
type Store interface {
	MCPHistoryStore
	WriteBatch(context.Context, []executionevidence.CompletedExchange) error
	ListRecentSummaries(context.Context, ExchangeFilter, Cursor, int) (ExchangeSummaryPage, error)
	GetExchange(context.Context, execution.ExchangeID) (executionevidence.CompletedExchange, bool, error)
	Maintain(context.Context, time.Time) error
	Close() error
}
