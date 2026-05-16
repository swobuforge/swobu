package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ResponseContinuityStore owns the minimal canonical continuation state
// required for truthful responses-style continuity across protocol boundaries.
// It supports native previous_response_id lookup and chain prefix matching inside
// one namespace so request-path orchestration can derive canonical last turns for
// * -> responses realization without pushing diff logic into adapters.
//
// MatchPrefix returns one deterministic best representative when several
// stored chains share the same best prefix content. For delta derivation, the
// semantic value is the matched canonical prefix itself, not which historical
// chain ID happened to carry it first.
//
// Store implementations may reconstruct full threads recursively from persisted
// turns and parent links; that mechanics stays behind this port.
type ResponseContinuityStore interface {
	Load(ctx context.Context, previousResponseID string) (canonical.ContinuitySnapshot, bool, error)
	MatchPrefix(ctx context.Context, namespace canonical.ContinuationNamespace, thread []canonical.CanonicalItem) (canonical.ContinuationPrefixMatch, bool, error)
	Store(ctx context.Context, namespace canonical.ContinuationNamespace, snapshot canonical.ContinuitySnapshot) error
}
