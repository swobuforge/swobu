package continuity

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

var ErrCheckpointExists = errors.New("checkpoint already exists")

// Commit is one immutable checkpoint plus optional semantic provenance.
type Commit struct {
	Checkpoint Checkpoint

	// ResolvedFrom identifies the exact checkpoint from which Checkpoint.Request
	// was materialized as an append-only continuation. When non-zero, the
	// retained history in Checkpoint.Request begins with that predecessor's
	// complete after-response history. Callers must leave it zero after any
	// history rewrite. Stores may ignore this provenance or use it only as a
	// storage optimization; it is never durable thread identity.
	ResolvedFrom canonical.SwobuResponseID
}

// Store retains immutable checkpoints inside workspace partitions. History
// lookup succeeds only when exactly one live checkpoint carries the key.
type Store interface {
	Get(context.Context, string, canonical.SwobuResponseID) (Checkpoint, bool, error)
	FindByHistory(context.Context, string, historyfingerprint.History) (Checkpoint, bool, error)
	Put(context.Context, string, Commit) error
}
