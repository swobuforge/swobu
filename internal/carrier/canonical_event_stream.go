package carrier

import "github.com/swobuforge/swobu/internal/domain/canonical"

// CanonicalEventStream is the internal response-truth carrier.
type CanonicalEventStream struct {
	Leg    Leg
	Events canonical.EventReader
	Meta   Meta
}
