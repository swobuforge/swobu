package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ContinuationStore is the semantic continuation port exposed to bootstrap and
// application wiring. It owns the wiring-facing contract while matching the
// canonical continuation shape.
type ContinuationStore interface {
	Put(ctx context.Context, rec canonical.ContinuationRecord) error
	Get(ctx context.Context, id canonical.ContinuationID) (canonical.ContinuationRecord, bool, error)
	Chain(ctx context.Context, id canonical.ContinuationID) ([]canonical.ContinuationRecord, error)
}
