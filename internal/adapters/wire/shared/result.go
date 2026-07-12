package shared

import (
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/exchange"
)

// WithAccumulatedEffects runs fn with a caller-owned effect accumulator and
// returns one typed exchange result with the collected effects.
func WithAccumulatedEffects[T any](fn func(effect.Sink) (T, error)) (exchange.Result[T], error) {
	var effects []effect.Effect
	value, err := fn(effect.AccumulatorSink{Effects: &effects})
	return exchange.NewResult(value, effects...), err
}
