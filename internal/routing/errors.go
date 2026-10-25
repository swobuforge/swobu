package routing

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidConfig       = errors.New("invalid routing config")
	ErrNotFound            = errors.New("not found")
	ErrConflict            = errors.New("conflict")
	ErrLastTarget          = errors.New("last target")
	ErrEmptyRequestedRoute = errors.New("requested route is empty")
	ErrNoRoute             = errors.New("workspace has no configured route")
)

type InvariantError struct {
	Path    string
	Message string
}

func (e *InvariantError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

func (e *InvariantError) Unwrap() error { return ErrInvalidConfig }

func pathError(path, message string) error {
	return &InvariantError{Path: path, Message: message}
}
