package wire

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// LoweredToolRecord records canonical source provenance identity, kind, and the
// count of target wire declarations emitted for it.
type LoweredToolRecord struct {
	Key           canonical.ToolKey
	Kind          canonical.ToolKind
	FragmentCount int
}

// LoweredToolSet records the sequence of lowered canonical tool declarations.
type LoweredToolSet struct {
	Records []LoweredToolRecord
}

// FindSource returns the record matching the given canonical tool key, if any.
func (s LoweredToolSet) FindSource(key canonical.ToolKey) (LoweredToolRecord, bool) {
	for _, record := range s.Records {
		if record.Key == key {
			return record, true
		}
	}
	return LoweredToolRecord{}, false
}

// Len returns the number of canonical tool records.
func (s LoweredToolSet) Len() int {
	return len(s.Records)
}

// TotalFragments returns the total count of target wire fragments emitted across all declarations.
func (s LoweredToolSet) TotalFragments() int {
	total := 0
	for _, record := range s.Records {
		total += record.FragmentCount
	}
	return total
}

// ResolveLoweredToolPolicy validates hard tool policy against the declarations
// that survived target lowering. A returned record is the sole fragment that a
// protocol-specific encoder must name for a specific policy.
func ResolveLoweredToolPolicy(policy canonical.ToolPolicy, lowered LoweredToolSet) (*LoweredToolRecord, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	switch policy.Mode {
	case canonical.ToolPolicyNone, canonical.ToolPolicyAuto:
		return nil, nil
	case canonical.ToolPolicyRequired:
		if lowered.Len() == 0 {
			return nil, canonical.BadRequest("required tool policy requires at least one declared tool")
		}
		if lowered.TotalFragments() == 0 {
			return nil, provider.NewIncompatibleTarget("target lowering produced no tool declarations to satisfy required tool policy")
		}
		return nil, nil
	case canonical.ToolPolicySpecific:
		specific, ok := policy.SpecificID()
		if !ok {
			return nil, canonical.BadRequest("specific tool policy requires a tool id")
		}
		record, ok := lowered.FindSource(specific)
		if !ok {
			return nil, canonical.BadRequest(fmt.Sprintf("tool %q is not present in the tool declaration set", specific))
		}
		if record.FragmentCount == 0 {
			return nil, provider.NewIncompatibleTarget(fmt.Sprintf("target lowering produced 0 fragments for tool %q", specific))
		}
		if record.FragmentCount > 1 {
			return nil, provider.NewIncompatibleTarget("specific tool policy requires exactly one target declaration")
		}
		return &record, nil
	default:
		return nil, canonical.BadRequest("tool policy is invalid")
	}
}
