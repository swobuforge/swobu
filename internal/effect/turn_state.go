package effect

// TurnStateOp describes one operational turn-state action.
type TurnStateOp string

const (
	TurnStateCapture    TurnStateOp = "capture"
	TurnStateReplay     TurnStateOp = "replay"
	TurnStateInvalidate TurnStateOp = "invalidate"
)

// TurnStateEffect records one operational state change or replay action.
type TurnStateEffect struct {
	Op    TurnStateOp `json:"op"`
	Key   string      `json:"key"`
	Value []byte      `json:"value,omitempty"`
}

func (TurnStateEffect) Kind() Kind { return KindTurnState }
