package exchange

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/wire"
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
	capture       *checkpointCaptureResponseStream
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

func (c *checkpointCommitter) commitDocument(ctx context.Context, response *historyfingerprint.Response) error {
	return c.commit(ctx, wire.ResponseCompletionSnapshot{State: wire.CompletionCompleted, ResponseFingerprint: response})
}

func (c *checkpointCommitter) commitIfReady(ctx context.Context, completion *wire.ResponseCompletion) error {
	if completion == nil {
		return nil
	}
	return c.commit(ctx, completion.Snapshot())
}

func (c *checkpointCommitter) commit(ctx context.Context, completion wire.ResponseCompletionSnapshot) error {
	if c == nil || completion.State != wire.CompletionCompleted {
		return nil
	}
	if c.capture == nil {
		return checkpointCommitError(errors.New("checkpoint commit is missing canonical response capture"))
	}
	captured := c.capture.snapshot()
	if captured.state != checkpointCaptureCompleted {
		if captured.err != nil {
			return checkpointCommitError(fmt.Errorf("checkpoint capture failed: %w", captured.err))
		}
		return checkpointCommitError(errors.New("checkpoint response capture did not complete"))
	}
	c.once.Do(func() {
		record := session.Checkpoint{
			Request: c.request.Clone(), Response: captured.response,
			ResolvedMedia: c.resolvedMedia.Clone(), CreatedAt: time.Now().UTC(),
		}
		if c.advance != nil && completion.ResponseFingerprint != nil {
			history, err := historyfingerprint.Advance(c.advance.Previous, c.advance.Request, *completion.ResponseFingerprint)
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
