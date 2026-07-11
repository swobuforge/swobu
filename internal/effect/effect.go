package effect

import "context"

type Kind string

const (
	KindObservation  Kind = "observation"
	KindStateCapture Kind = "state_capture"
	KindStateReplay  Kind = "state_replay"
	KindLoss         Kind = "loss"
)

type Effect interface {
	Kind() Kind
}

type Sink interface {
	Commit(ctx context.Context, exchangeID string, effects []Effect) error
}
