package continuity

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
)

// Checkpoint is one logically self-contained immutable successful-response
// boundary. Stores may structurally share Request history internally.
type Checkpoint struct {
	History           *historyfingerprint.History
	ExecutionAffinity executionaffinity.Key
	Request           canonical.CanonicalRequest
	Response          canonical.CanonicalResponse
}

func (r Checkpoint) Clone() Checkpoint {
	cloned := Checkpoint{ExecutionAffinity: r.ExecutionAffinity, Request: r.Request.Clone(), Response: r.Response.Clone()}
	if r.History != nil {
		history := *r.History
		cloned.History = &history
	}
	return cloned
}
