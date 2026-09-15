package continuity

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

var ErrCheckpointExists = errors.New("checkpoint already exists")

// StorageReuse is opaque evidence already established by continuity
// resolution. Its sealed token is owned by the producing Store backend.
type StorageReuse struct {
	token        storageReuseToken
	history      *historyfingerprint.History
	prefixLength int
}

type storageReuseToken interface{ isStorageReuseToken() }

// ReuseCheckpoint identifies an exact selected checkpoint's physical tail.
func ReuseCheckpoint(checkpoint Checkpoint) StorageReuse {
	return checkpoint.storageReuse
}

// ReuseVisibleHistory records the codec-declared historical prefix boundary.
// The length counts retained canonical items, not request-scoped prelude.
func ReuseVisibleHistory(history historyfingerprint.History, prefixLength int) StorageReuse {
	return StorageReuse{history: &history, prefixLength: prefixLength}
}

// Commit is the immutable checkpoint and its optional storage-only reuse
// evidence written at one successful response boundary.
type Commit struct {
	Checkpoint Checkpoint
	Reuse      StorageReuse
}

// Store retains immutable checkpoints inside workspace partitions. History
// lookup succeeds only when exactly one live checkpoint carries the key.
type Store interface {
	Get(context.Context, string, canonical.SwobuResponseID) (Checkpoint, bool, error)
	FindByHistory(context.Context, string, historyfingerprint.History) (Checkpoint, bool, error)
	Put(context.Context, string, Commit) error
}
