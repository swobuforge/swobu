package session

import (
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

const defaultCheckpointTTL = 24 * time.Hour

// Checkpoint is one self-contained immutable successful-response boundary
// retained for process-local session resumption. Its identity is the Swobu ID
// in Response; no duplicate checkpoint or session ID is stored. Request is the
// complete effective request, not a delta. ResolvedMedia is semantic resumption
// state because later execution must not refetch mutable external URLs.
//
// Intentionally absent:
//   - Status (a checkpoint implies completed)
//   - Attachment bag
//   - Mutable session head
//   - Parent or request-delta graph
type Checkpoint struct {
	// HistoryFingerprint is the optional completed visible-history value used
	// for exact implicit lookup. Its absence never prevents explicit resumption.
	HistoryFingerprint *historyfingerprint.History
	Request            canonical.CanonicalRequest
	Response           canonical.CanonicalResponse
	ResolvedMedia      ResolvedMedia
	CreatedAt          time.Time
	// ExpiresAt bounds how long the checkpoint remains resumable.
	ExpiresAt *time.Time
}

// Clone returns a deep copy suitable for crossing the storage boundary.
func (r Checkpoint) Clone() Checkpoint {
	cloned := Checkpoint{
		Request:       r.Request.Clone(),
		Response:      r.Response.Clone(),
		ResolvedMedia: r.ResolvedMedia.Clone(),
		CreatedAt:     r.CreatedAt,
	}
	if r.HistoryFingerprint != nil {
		history := *r.HistoryFingerprint
		cloned.HistoryFingerprint = &history
	}
	if r.ExpiresAt != nil {
		expiresAt := *r.ExpiresAt
		cloned.ExpiresAt = &expiresAt
	}
	return cloned
}
