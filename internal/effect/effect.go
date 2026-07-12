package effect

import "context"

// Kind identifies one effect family.
type Kind string

const (
	KindCompatibility Kind = "compatibility"
	KindTurnState     Kind = "turn_state"
)

// Effect is one typed side effect emitted by the exchange runtime.
type Effect interface {
	Kind() Kind
}

// Sink commits one exchange's effects to the configured evidence or state
// backends.
type Sink interface {
	Commit(ctx context.Context, exchangeID string, effects []Effect) error
}
