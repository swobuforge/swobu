package session

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

var (
	ErrCheckpointExists      = errors.New("session checkpoint already exists")
	ErrSessionExists         = errors.New("client session already exists")
	ErrStaleSessionHead      = errors.New("client session head changed")
	ErrSessionSchemeMismatch = errors.New("client session codec scheme changed")
)

// ClientSessionID is one process-local client history lineage.
type ClientSessionID string

// ClientSession records the current checkpoint head for one codec scheme.
type ClientSession struct {
	ID     ClientSessionID
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

// Store retains immutable checkpoints and one atomic current head per session
// lineage inside workspace partitions.
type Store interface {
	Get(context.Context, string, canonical.SwobuResponseID) (Checkpoint, bool, error)
	IsCurrentHead(context.Context, string, ClientSessionID, canonical.SwobuResponseID) (bool, error)
	ResolveHeadByHistory(context.Context, string, historyfingerprint.History) (Checkpoint, HistoryResolution, error)
	StartSession(context.Context, string, Checkpoint) (ClientSession, error)
	AdvanceSession(context.Context, string, ClientSessionID, canonical.SwobuResponseID, Checkpoint) error
}
