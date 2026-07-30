package exchange

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/session"
)

// checkpointCommitter joins canonical capture with the optional codec history
// leaf. A successful client-visible response ID is gated on its canonical
// checkpoint because client projections can omit continuation-critical opaque
// reasoning and resolved media. Client wire storage hints never participate;
// only history-fingerprint composition is best effort.
type checkpointCommitter struct {
	once sync.Once
	err  error

	exchangeID    string
	workspaceSlug string
	store         session.Store
	request       canonical.CanonicalRequest
	resolvedMedia session.ResolvedMedia
	advance       *historyAdvance
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
		record := session.Checkpoint{
			Request: c.request.Clone(), Response: response.Clone(),
			ResolvedMedia: c.resolvedMedia.Clone(), CreatedAt: time.Now().UTC(),
		}
		if c.advance != nil && fingerprint != nil {
			history, err := historyfingerprint.Advance(c.advance.Previous, c.advance.Request, *fingerprint)
			if err != nil {
				c.logHistoryComposeFailure(err)
			} else {
				record.HistoryFingerprint = &history
			}
		}
		if err := c.store.Put(ctx, c.workspaceSlug, record); err != nil {
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
