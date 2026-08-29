package provider

// TargetFact is the closed set of empirically learnable target-dialect
// questions. Each value selects between two codec-owned wire projections; it
// never describes semantic capability, model quality, or routing preference.
type TargetFact uint8

const (
	AcceptsParallelToolCallsFalse TargetFact = iota + 1
	AcceptsMaxCompletionTokens
	AcceptsReasoningEffortMax
	AcceptsReasoningDisabled
	AcceptsFunctionCallOutputArray
)

// TargetFactLookup reads process-scoped knowledge for one target generation.
// Absence is not rejection: TargetFacts uses the preferred representation.
type TargetFactLookup func(TargetFact) (bool, bool)

// TargetFacts is one attempt-private dialect reader. Repeated reads are stable
// and Reads returns the exact branch values that influenced encoding.
type TargetFacts struct {
	lookup TargetFactLookup
	reads  map[TargetFact]bool
}

func NewTargetFacts(lookup TargetFactLookup) *TargetFacts {
	return &TargetFacts{lookup: lookup, reads: make(map[TargetFact]bool)}
}

func (f *TargetFacts) read(fact TargetFact) bool {
	if f == nil {
		return true
	}
	if value, ok := f.reads[fact]; ok {
		return value
	}
	value := true
	if f.lookup != nil {
		if known, ok := f.lookup(fact); ok {
			value = known
		}
	}
	f.reads[fact] = value
	return value
}

func (f *TargetFacts) AcceptsParallelToolCallsFalse() bool {
	return f.read(AcceptsParallelToolCallsFalse)
}
func (f *TargetFacts) AcceptsMaxCompletionTokens() bool { return f.read(AcceptsMaxCompletionTokens) }
func (f *TargetFacts) AcceptsReasoningEffortMax() bool  { return f.read(AcceptsReasoningEffortMax) }
func (f *TargetFacts) AcceptsReasoningDisabled() bool   { return f.read(AcceptsReasoningDisabled) }
func (f *TargetFacts) AcceptsFunctionCallOutputArray() bool {
	return f.read(AcceptsFunctionCallOutputArray)
}

// Reads returns a detached snapshot of every fact and value used by encoding.
func (f *TargetFacts) Reads() map[TargetFact]bool {
	if f == nil {
		return nil
	}
	out := make(map[TargetFact]bool, len(f.reads))
	for fact, value := range f.reads {
		out[fact] = value
	}
	return out
}
