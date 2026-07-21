package session

import (
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/historyfingerprint"
	"github.com/swobuforge/swobu/internal/domain/responsesnative"
)

const defaultCheckpointTTL = 24 * time.Hour

// Checkpoint is one immutable successful-response boundary retained for session
// resumption. Its identity is the Swobu ID in Response; no duplicate checkpoint
// or session ID is stored.
//
// Intentionally absent:
//   - Status (a checkpoint implies completed)
//   - Attachment bag
//   - Mutable session head
type Checkpoint struct {
	// Predecessor identifies the immutable checkpoint whose response immediately
	// precedes this invocation. It exists only to recover optional protocol-native
	// replay history, not to infer canonical assistant-turn topology.
	Predecessor *canonical.SwobuResponseID
	// InputSegment is this invocation's client contribution, without a native
	// previous-response selector.
	InputSegment canonical.CanonicalRequest
	// ResponsesInput retains client-supplied native fields on this invocation's
	// object-form input items.
	ResponsesInput responsesnative.Items
	// ResponsesOutput is the exact native output batch produced by this
	// checkpoint's Responses backend response.
	ResponsesOutput responsesnative.Items
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
		InputSegment:    r.InputSegment.Clone(),
		ResponsesInput:  r.ResponsesInput.Clone(),
		ResponsesOutput: r.ResponsesOutput.Clone(),
		Request:         r.Request.Clone(),
		Response:        r.Response.Clone(),
		ResolvedMedia:   r.ResolvedMedia.Clone(),
		CreatedAt:       r.CreatedAt,
	}
	if r.Predecessor != nil {
		predecessor := *r.Predecessor
		cloned.Predecessor = &predecessor
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
