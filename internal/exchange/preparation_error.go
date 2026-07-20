package exchange

import (
	"errors"
	"fmt"
)

type PreparationErrorScope string

const (
	PreparationRequest   PreparationErrorScope = "request"
	PreparationCandidate PreparationErrorScope = "candidate"
)

type PreparationError struct {
	Scope PreparationErrorScope
	Err   error
}

func (e PreparationError) Error() string { return e.Err.Error() }
func (e PreparationError) Unwrap() error { return e.Err }

func preparationError(scope PreparationErrorScope, format string, args ...any) error {
	return PreparationError{Scope: scope, Err: fmt.Errorf(format, args...)}
}

func preparationErrorScope(err error) PreparationErrorScope {
	var scoped PreparationError
	if errors.As(err, &scoped) {
		return scoped.Scope
	}
	return PreparationRequest
}
