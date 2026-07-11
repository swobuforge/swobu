package carrier

import "github.com/swobuforge/swobu/internal/domain/canonical"

// CanonicalRequestSnapshot is one semantic request carrier plus boundary
// metadata.
type CanonicalRequestSnapshot struct {
	Request canonical.CanonicalRequest
	Meta    Meta
}
