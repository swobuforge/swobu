package effect

import "github.com/swobuforge/swobu/internal/compat"

// CompatibilityEffect records one semantic preservation decision from an exchange
// boundary.
type CompatibilityEffect struct {
	Feature compat.Feature `json:"feature"`
	Outcome compat.Outcome `json:"outcome"`
	Subject compat.Subject `json:"subject,omitempty"`
}

func (CompatibilityEffect) Kind() Kind { return KindCompatibility }
