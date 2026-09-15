package continuity

import (
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

const defaultCheckpointTTL = 24 * time.Hour

// Checkpoint is one logically self-contained immutable successful-response
// boundary. Stores may structurally share Request history internally.
type Checkpoint struct {
	History           *historyfingerprint.History
	ExecutionAffinity executionaffinity.Key
	Request           canonical.CanonicalRequest
	Response          canonical.CanonicalResponse
	CreatedAt         time.Time
	ExpiresAt         *time.Time
	storageReuse      StorageReuse
}

func (r Checkpoint) Clone() Checkpoint {
	cloned := Checkpoint{ExecutionAffinity: r.ExecutionAffinity, Request: r.Request.Clone(), Response: r.Response.Clone(), CreatedAt: r.CreatedAt, storageReuse: r.storageReuse}
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
