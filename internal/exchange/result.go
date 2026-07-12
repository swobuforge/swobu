package exchange

import (
	"github.com/swobuforge/swobu/internal/effect"
)

// Result carries the next exchange-boundary value plus any side effects the
// step wants committed outside the step itself.
type Result[T any] struct {
	Value   T
	Effects []effect.Effect
}

// NewResult clones the provided effects into one typed exchange-boundary result.
func NewResult[T any](value T, effects ...effect.Effect) Result[T] {
	return Result[T]{
		Value:   value,
		Effects: append([]effect.Effect(nil), effects...),
	}
}
