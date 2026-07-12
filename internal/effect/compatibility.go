package effect

import "github.com/swobuforge/swobu/internal/compat"

// Compatibility records one semantic preservation decision from an exchange
// boundary.
type Compatibility struct {
	Feature  compat.Feature  `json:"feature"`
	Outcome  compat.Outcome  `json:"outcome"`
	Subject  compat.Subject  `json:"subject,omitempty"`
}

func (Compatibility) Kind() Kind { return KindCompatibility }

