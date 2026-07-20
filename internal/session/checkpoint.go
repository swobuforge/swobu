package session

import (
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

const defaultCheckpointTTL = 24 * time.Hour

// Checkpoint is one immutable successful-response boundary retained for session
// resumption. Its identity is the Swobu ID in Response; no duplicate checkpoint
// or session ID is stored.
//
// Intentionally absent:
//   - Parent (no chain)
//   - Status (a checkpoint implies completed)
//   - RequestDelta (full request is stored, not deltas)
//   - Attachment bag
//   - Mutable session head
type Checkpoint struct {
	Request       canonical.CanonicalRequest
	Response      canonical.CanonicalResponse
	ResolvedMedia ResolvedMedia
	CreatedAt     time.Time
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
	if r.ExpiresAt != nil {
		expiresAt := *r.ExpiresAt
		cloned.ExpiresAt = &expiresAt
	}
	return cloned
}
