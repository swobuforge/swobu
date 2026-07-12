package stage

import "github.com/swobuforge/swobu/internal/effect"

// Result carries one stage execution result: the next value, mutation truth,
// and any emitted effects.
type Result[T any] struct {
	Value   T
	Mutated bool
	Effects []effect.Effect
}
