package shared

import "github.com/swobuforge/swobu/internal/compat"

// WithAccumulatedDecisions runs one codec operation with a local typed
// compatibility collector. The value, decisions, and error remain separate;
// there is no generic functional result container.
func WithAccumulatedDecisions[T any](fn func(compat.Sink) (T, error)) (T, []compat.Decision, error) {
	var decisions []compat.Decision
	value, err := fn(compat.AccumulatorSink{Decisions: &decisions})
	return value, decisions, err
}
