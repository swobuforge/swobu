package compat

// Classification is the terminal semantic treatment of one successful
// exchange. Exact is derived from an empty effective trace.
type Classification uint8

const (
	ClassificationExact Classification = iota + 1
	ClassificationApproximate
	ClassificationPolyfilled
)

// Summary is the immutable effective compatibility truth for one successful
// exchange. Failed-candidate changes never enter it.
type Summary struct {
	Classification Classification
	Changes        []Change
}

func Summarize(changes []Change, polyfilled bool) Summary {
	classification := ClassificationExact
	if len(changes) > 0 {
		classification = ClassificationApproximate
	}
	if polyfilled {
		classification = ClassificationPolyfilled
	}
	return Summary{Classification: classification, Changes: CloneChanges(changes)}
}

func (s Summary) Clone() Summary {
	s.Changes = CloneChanges(s.Changes)
	return s
}
