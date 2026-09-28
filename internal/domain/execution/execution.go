// Package execution owns the small, resource-independent execution vocabulary.
package execution

type Outcome string

const (
	Succeeded Outcome = "succeeded"
	Failed    Outcome = "failed"
	Cancelled Outcome = "cancelled"
)

func (o Outcome) Valid() bool { return o == Succeeded || o == Failed || o == Cancelled }

type Occurrence string

const (
	DidNotOccur     Occurrence = "did_not_occur"
	MayHaveOccurred Occurrence = "may_have_occurred"
	Occurred        Occurrence = "occurred"
)

func (o Occurrence) Valid() bool { return o == DidNotOccur || o == MayHaveOccurred || o == Occurred }
