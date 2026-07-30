package compat

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// Kind is the closed set of successful non-exact semantic transformations.
type Kind uint8

const (
	Approximation Kind = iota + 1
	Omission
)

// Change records one successful non-exact semantic transformation. Execution
// and attempt context remain with exchange; exact lowering has no Change.
type Change struct {
	Capability canonical.CapabilityPath
	Occurrence canonical.Occurrence
	Kind       Kind
	Preserved  canonical.CapabilityPath
}

func NewApproximation(capability, preserved canonical.CapabilityPath, occurrence canonical.Occurrence) Change {
	if capability == "" || preserved == "" {
		panic("compatibility approximation requires changed and preserved capabilities")
	}
	return Change{Capability: capability, Occurrence: occurrence, Kind: Approximation, Preserved: preserved}
}

func NewOmission(capability canonical.CapabilityPath, occurrence canonical.Occurrence) Change {
	if capability == "" {
		panic("compatibility omission requires a capability")
	}
	return Change{Capability: capability, Occurrence: occurrence, Kind: Omission}
}

// NewChange constructs one non-exact change for traversal helpers.
func NewChange(capability canonical.CapabilityPath, kind Kind, occurrence canonical.Occurrence) Change {
	switch kind {
	case Approximation:
		return NewApproximation(capability, capability, occurrence)
	case Omission:
		return NewOmission(capability, occurrence)
	default:
		panic("compatibility change requires approximation or omission")
	}
}

func (c Change) Validate() error {
	if c.Capability == "" {
		return fmt.Errorf("compatibility change capability is empty")
	}
	switch c.Kind {
	case Approximation:
		if c.Preserved == "" {
			return fmt.Errorf("compatibility approximation preserved capability is empty")
		}
	case Omission:
		if c.Preserved != "" {
			return fmt.Errorf("compatibility omission cannot declare preserved capability")
		}
	default:
		return fmt.Errorf("compatibility change kind is invalid")
	}
	return nil
}

func CloneChanges(changes []Change) []Change {
	return append([]Change(nil), changes...)
}

func ValidateChanges(changes []Change) error {
	for index, change := range changes {
		if err := change.Validate(); err != nil {
			return fmt.Errorf("compatibility change %d: %w", index, err)
		}
	}
	return nil
}

// AppendUnique keeps one semantic fact per capability occurrence and treatment.
// Progressive codecs may observe the same loss in both a delta and terminal
// snapshot; that is one change, not two.
func AppendUnique(changes []Change, change Change) []Change {
	for _, existing := range changes {
		if existing.Capability == change.Capability &&
			existing.Occurrence.Key() == change.Occurrence.Key() &&
			existing.Kind == change.Kind &&
			existing.Preserved == change.Preserved {
			return changes
		}
	}
	return append(changes, change)
}
