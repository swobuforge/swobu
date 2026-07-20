package session

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

// ErrCheckpointExists reports that Put was asked to overwrite an immutable
// checkpoint under the same workspace-scoped response ID.
var ErrCheckpointExists = errors.New("session checkpoint already exists")

// Store persists immutable checkpoints in workspace-slug partitions. The
// caller supplies the validated slug resolved from the request URL; the store
// owns no caller identity, mutable session head, or generic namespace.
type Store interface {
	// Get returns one unexpired checkpoint by workspace slug and response ID.
	// Expired checkpoints are never returned; the bool indicates whether an
	// available checkpoint was found.
	Get(ctx context.Context, workspaceSlug string, id canonical.SwobuResponseID) (Checkpoint, bool, error)
	// FindByHistory performs one exact workspace-local secondary-index
	// lookup. It never searches prefixes, ancestors, or canonical history.
	FindByHistory(ctx context.Context, workspaceSlug string, history historyfingerprint.History) (Checkpoint, bool, error)
	// Put writes one immutable checkpoint. Duplicate response IDs are rejected.
	Put(ctx context.Context, workspaceSlug string, checkpoint Checkpoint) error
}
