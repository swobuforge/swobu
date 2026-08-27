package wire

import "github.com/swobuforge/swobu/internal/domain/canonical"

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
