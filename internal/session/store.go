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

// HistoryMatch is the closed result of one exact visible-history lookup.
// Ambiguous is deliberately distinct from missing: an indistinguishable
// visible projection must never select arbitrary hidden checkpoint state.
type HistoryMatch struct {
	kind       historyMatchKind
	checkpoint Checkpoint
}

type historyMatchKind uint8

const (
	historyMatchMissing historyMatchKind = iota + 1
	historyMatchUnique
	historyMatchAmbiguous
)

func MissingHistoryMatch() HistoryMatch { return HistoryMatch{kind: historyMatchMissing} }

func UniqueHistoryMatch(checkpoint Checkpoint) HistoryMatch {
	cloned := checkpoint.Clone()
	return HistoryMatch{kind: historyMatchUnique, checkpoint: cloned}
}

func AmbiguousHistoryMatch() HistoryMatch { return HistoryMatch{kind: historyMatchAmbiguous} }

func (m HistoryMatch) IsMissing() bool {
	return m.kind == historyMatchMissing
}

func (m HistoryMatch) IsAmbiguous() bool {
	return m.kind == historyMatchAmbiguous
}

func (m HistoryMatch) Unique() (Checkpoint, bool) {
	if m.kind != historyMatchUnique {
		return Checkpoint{}, false
	}
	return m.checkpoint.Clone(), true
}

// Store persists immutable checkpoints in workspace-slug partitions. The
// caller supplies the validated slug resolved from the request URL; the store
// owns no caller identity, mutable session head, or generic namespace.
type Store interface {
	// Get returns one unexpired checkpoint by workspace slug and response ID.
	// Expired checkpoints are never returned; the bool indicates whether an
	// available checkpoint was found.
	Get(ctx context.Context, workspaceSlug string, id canonical.SwobuResponseID) (Checkpoint, bool, error)
	// FindByHistory performs one exact workspace-local secondary-index lookup.
	// It never searches prefixes, ancestors, or canonical history, and it never
	// chooses among checkpoints with indistinguishable visible histories.
	FindByHistory(ctx context.Context, workspaceSlug string, history historyfingerprint.History) (HistoryMatch, error)
	// Put writes one immutable checkpoint. Duplicate response IDs are rejected.
	Put(ctx context.Context, workspaceSlug string, checkpoint Checkpoint) error
}
