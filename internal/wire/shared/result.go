package shared

import (
	"github.com/swobuforge/swobu/internal/effect"
)

// WithAccumulatedEffects runs fn with a caller-owned effect accumulator and
// returns one typed effect result with the collected effects.
func WithAccumulatedEffects[T any](fn func(effect.Sink) (T, error)) (effect.Result[T], error) {
	var effects []effect.Effect
	value, err := fn(effect.AccumulatorSink{Effects: &effects})
	return effect.NewResult(value, effects...), err
}
