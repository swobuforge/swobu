package compat

import "fmt"

// CompatibilityMode selects whether a semantic approximation is rejected or
// retained with explicit evidence. Exact carrier conversion is legal in both modes.
type CompatibilityMode string

const (
	CompatibilityStrict CompatibilityMode = "strict"
	CompatibilityCompat CompatibilityMode = "compat"
)

// CompatibilityPolicy is resolved once for an exchange. The zero value uses
// the local-first compatibility default.
type CompatibilityPolicy struct{ Mode CompatibilityMode }

func (p CompatibilityPolicy) EffectiveMode() CompatibilityMode {
	if p.Mode == CompatibilityStrict {
		return CompatibilityStrict
	}
	return CompatibilityCompat
}

func (p CompatibilityPolicy) Validate() error {
	if p.Mode != "" && p.Mode != CompatibilityStrict && p.Mode != CompatibilityCompat {
		return fmt.Errorf("compatibility mode %q is invalid", p.Mode)
	}
	return nil
}
