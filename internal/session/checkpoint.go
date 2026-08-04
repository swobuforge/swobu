package session

import (
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

const defaultCheckpointTTL = 24 * time.Hour

// Checkpoint is one self-contained immutable successful-response boundary.
// Request is always the complete effective canonical request. HistoryScheme
// identifies the immutable client-codec lineage. History is an optional
// visible-history digest used only for implicit lookup while this checkpoint
// is the current session head.
type Checkpoint struct {
	ID            canonical.SwobuResponseID
	SessionID     ClientSessionID
	HistoryScheme historyfingerprint.Scheme
	History       *historyfingerprint.History
	Request       canonical.CanonicalRequest
	Response      canonical.CanonicalResponse
	CreatedAt     time.Time
	ExpiresAt     *time.Time
}

func (r Checkpoint) Clone() Checkpoint {
	cloned := Checkpoint{
		ID: r.ID, SessionID: r.SessionID, HistoryScheme: r.HistoryScheme, Request: r.Request.Clone(),
		Response: r.Response.Clone(), CreatedAt: r.CreatedAt,
	}
	if r.History != nil {
		history := *r.History
		cloned.History = &history
	}
	if r.ExpiresAt != nil {
		expiresAt := *r.ExpiresAt
		cloned.ExpiresAt = &expiresAt
	}
	return cloned
}
