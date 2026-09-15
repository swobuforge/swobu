package exchange

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

// checkpointCommitter joins canonical capture with an optional history leaf,
// then atomically stores one immutable checkpoint. A successful
// client-visible response ID is gated on its canonical checkpoint because
// client projections can omit continuation-critical opaque reasoning. Client
// wire storage hints never participate. Fingerprint composition is best effort;
// failure leaves the response-ID checkpoint explicitly resumable but
// unindexed for implicit history lookup.
type checkpointCommitter struct {
	once sync.Once
	err  error

	exchangeID        string
	workspaceSlug     string
	store             continuity.Store
	request           canonical.CanonicalRequest
	advance           *historyAdvance
	executionAffinity executionaffinity.Key
	storageReuse      continuity.StorageReuse
}

// CheckpointCommitError identifies failure to make a client-visible response
// ID addressable. Delivery adapters use the type to preserve terminal truth.
type CheckpointCommitError struct{ err error }

func (e CheckpointCommitError) Error() string {
	if e.err == nil {
		return "checkpoint commit failed"
	}
	return e.err.Error()
}
func (e CheckpointCommitError) Unwrap() error { return e.err }

func checkpointCommitError(err error) error {
	if err == nil {
		return nil
	}
	return CheckpointCommitError{err: err}
}

func (c *checkpointCommitter) commitDocument(ctx context.Context, response canonical.CanonicalResponse, fingerprint *historyfingerprint.Response) error {
	if c == nil {
		return nil
	}
	c.once.Do(func() {
		record := continuity.Checkpoint{Request: c.request.Clone(), Response: response.Clone(), ExecutionAffinity: c.executionAffinity, CreatedAt: time.Now().UTC()}
		if c.advance != nil && fingerprint != nil {
			history, err := historyfingerprint.Advance(c.advance.Previous, c.advance.Request, *fingerprint)
			if err != nil {
				c.logHistoryComposeFailure(err)
			} else {
				record.History = &history
			}
		}
		err := c.store.Put(ctx, c.workspaceSlug, continuity.Commit{Checkpoint: record, Reuse: c.storageReuse})
		if err != nil {
			c.logFailure("store", err)
			c.err = checkpointCommitError(fmt.Errorf("checkpoint store failed: %w", err))
		}
	})
	return c.err
}

func (c *checkpointCommitter) logHistoryComposeFailure(err error) {
	slog.Warn("history fingerprint composition skipped",
		"component", "exchange",
		"event", "history_fingerprint_compose_failed",
		"exchange_id", c.exchangeID,
		"workspace", c.workspaceSlug,
		"error", err,
	)
}

func (c *checkpointCommitter) logFailure(stage string, err error) {
	slog.Error("checkpoint commit failed",
		"component", "exchange",
		"event", "checkpoint_commit_failed",
		"exchange_id", c.exchangeID,
		"workspace", c.workspaceSlug,
		"stage", stage,
		"error", err,
	)
}
