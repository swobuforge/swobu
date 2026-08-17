package trafficevidence

import "fmt"

// ReusablePrefixState is the closed durable truth vocabulary for whether the
// previous model-visible reusable prefix survived the current request.
type ReusablePrefixState string

const (
	ReusablePrefixUnknown   ReusablePrefixState = "unknown"
	ReusablePrefixPreserved ReusablePrefixState = "preserved"
	ReusablePrefixChanged   ReusablePrefixState = "changed"
	ReusablePrefixNative    ReusablePrefixState = "native_continuation"
)

// ReusablePrefixChangeKind classifies the first changed semantic owner. The
// detailed canonical occurrence remains ephemeral in Exchange logs.
type ReusablePrefixChangeKind string

const (
	ReusablePrefixInstruction ReusablePrefixChangeKind = "instruction"
	ReusablePrefixTool        ReusablePrefixChangeKind = "tool"
	ReusablePrefixInput       ReusablePrefixChangeKind = "input"
)

// ReusablePrefixEvidence is the complete persisted reusable-prefix fact. A
// change kind exists only for Changed evidence.
type ReusablePrefixEvidence struct {
	state      ReusablePrefixState
	changeKind ReusablePrefixChangeKind
}

func UnknownReusablePrefix() ReusablePrefixEvidence {
	return ReusablePrefixEvidence{state: ReusablePrefixUnknown}
}

func PreservedReusablePrefix() ReusablePrefixEvidence {
	return ReusablePrefixEvidence{state: ReusablePrefixPreserved}
}

func NativeReusablePrefix() ReusablePrefixEvidence {
	return ReusablePrefixEvidence{state: ReusablePrefixNative}
}

func NewChangedReusablePrefix(kind ReusablePrefixChangeKind) (ReusablePrefixEvidence, error) {
	if kind != ReusablePrefixInstruction && kind != ReusablePrefixTool && kind != ReusablePrefixInput {
		return ReusablePrefixEvidence{}, fmt.Errorf("reusable-prefix change kind is invalid")
	}
	return ReusablePrefixEvidence{state: ReusablePrefixChanged, changeKind: kind}, nil
}

func (e ReusablePrefixEvidence) State() ReusablePrefixState {
	if e.state == "" {
		return ReusablePrefixUnknown
	}
	return e.state
}

func (e ReusablePrefixEvidence) ChangeKind() (ReusablePrefixChangeKind, bool) {
	return e.changeKind, e.State() == ReusablePrefixChanged
}
