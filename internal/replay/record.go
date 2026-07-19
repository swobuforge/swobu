package replay

import (
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

const defaultRecordTTL = 24 * time.Hour

// Record is the full semantic request state persisted for replay.
// A record exists only if replay capture succeeded at terminal success.
//
// Intentionally absent:
//   - Parent (no chain)
//   - Status (a record implies completed)
//   - Replayable bool (presence implies replayable)
//   - RequestDelta (full request is stored, not deltas)
//   - Attachment bag
//   - Continuation namespace
type Record struct {
	ID        ID
	Request   canonical.CanonicalRequest
	Response  canonical.CanonicalOutputProjection
	Native    *provider.NativeContinuation
	CreatedAt time.Time
	// ExpiresAt bounds how long the record remains replay-addressable.
	ExpiresAt *time.Time
}

// Clone returns a deep copy of the replay record suitable for store handoff.
func (r Record) Clone() Record {
	cloned := Record{
		ID:        r.ID,
		Request:   r.Request.Clone(),
		Response:  r.Response.CloneProjection(),
		CreatedAt: r.CreatedAt,
	}
	if r.ExpiresAt != nil {
		expiresAt := *r.ExpiresAt
		cloned.ExpiresAt = &expiresAt
	}
	if r.Native != nil {
		native := *r.Native
		cloned.Native = &native
	}
	return cloned
}
