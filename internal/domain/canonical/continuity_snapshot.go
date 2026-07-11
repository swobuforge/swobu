package canonical

import "strings"

type ContinuationNamespace string

func (s ContinuationNamespace) IsZero() bool {
	return strings.TrimSpace(string(s)) == "" // swobu:io-string source=domain
}

func (s ContinuationNamespace) String() string {
	return string(s)
}

type ContinuationPrefixMatch struct {
	Snapshot     ContinuitySnapshot
	PrefixLength int
}

// ContinuitySnapshot is the minimal replayable canonical state Swobu keeps so
// response chains can be resumed without leaking backend wire history rules.
type ContinuitySnapshot struct {
	ResponseID string
	Model      string
	Thread     []CanonicalItem
}

func NewContinuitySnapshot(responseID string, model string, thread []CanonicalItem) ContinuitySnapshot {
	return ContinuitySnapshot{
		ResponseID: responseID,
		Model:      model,
		Thread:     cloneCanonicalItems(thread),
	}
}

func (s ContinuitySnapshot) Clone() ContinuitySnapshot {
	return NewContinuitySnapshot(s.ResponseID, s.Model, s.Thread)
}
