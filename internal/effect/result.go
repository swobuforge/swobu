package effect

// Result carries one boundary step's computed value plus any side effects the
// step wants committed outside the step itself. It is deliberately thin: a
// value and an effect slice with no transform behavior.
type Result[T any] struct {
	Value   T
	Effects []Effect
}

// NewResult clones the provided effects into one typed boundary result.
func NewResult[T any](value T, effects ...Effect) Result[T] {
	return Result[T]{
		Value:   value,
		Effects: append([]Effect(nil), effects...),
	}
}
