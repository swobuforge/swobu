package continuity

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/thread"
)

var (
	ErrCheckpointExists     = errors.New("thread checkpoint already exists")
	ErrThreadExists         = errors.New("thread already exists")
	ErrStaleThreadHead      = errors.New("thread head changed")
	ErrThreadSchemeMismatch = errors.New("thread client codec scheme changed")
)

// Thread records the current checkpoint head for one codec scheme.
type Thread struct {
	ID     thread.ID
	Scheme historyfingerprint.Scheme
	Head   canonical.SwobuResponseID
}

// HistoryResolution is the closed result of exact current-head lookup.
type HistoryResolution uint8

const (
	HistoryNotFound HistoryResolution = iota
	HistoryUniqueHead
	HistoryAmbiguous
)

// Store retains immutable checkpoints and one atomic current head per Thread
// inside workspace partitions.
type Store interface {
	GetCheckpoint(context.Context, string, canonical.SwobuResponseID) (Checkpoint, bool, error)
	GetThread(context.Context, string, thread.ID) (Thread, bool, error)
	IsCurrentHead(context.Context, string, thread.ID, canonical.SwobuResponseID) (bool, error)
	ResolveHeadByHistory(context.Context, string, historyfingerprint.History, thread.ID) (Checkpoint, HistoryResolution, error)
	StartThread(context.Context, string, Checkpoint) (Thread, error)
	AdvanceThread(context.Context, string, thread.ID, canonical.SwobuResponseID, Checkpoint) error
}
