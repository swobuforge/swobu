package configstore

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/routing"
)

// ReviewedConnection is a transient publication precondition; it is never
// persisted as a second copy of the reusable Connection state.
type ReviewedConnection struct {
	ID       string
	Revision string
}

// ReferencePublisher checks the candidate under the dependent owner's lock and
// executes commit before releasing it. Lock order is routing -> Connection ->
// Node file; the callback must not reenter either publication store.
type ReferencePublisher func(routing.Config, *ReviewedConnection, func() error) error

// InstallReferenceValidator binds dependent identity admission once during
// daemon assembly. Every candidate and its disk publication are one guarded
// critical section. The empty initial publication only validates current state.
// Install before serving commands; replacement/removal is forbidden. The
// callback must commit exactly once on success and must not reenter this store.
func (s *Store) InstallReferenceValidator(validate ReferencePublisher) error {
	if s == nil {
		return ErrStoreClosed
	}
	if validate == nil {
		return errors.New("reference validator is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == storeClosed {
		return ErrStoreClosed
	}
	if s.referenceValidator != nil {
		return errors.New("reference validator already installed")
	}
	if err := validate(s.snapshot.Load().Clone(), nil, func() error { return nil }); err != nil {
		return err
	}
	s.referenceValidator = validate
	return nil
}

// InspectLocked holds the routing publication boundary while an application
// inspects the current detached snapshot and commits a dependent operation,
// such as deleting an unused Connection. No routing bytes, revisions, receipts
// or audit facts are changed. The callback must not reenter this store.
//
// This excludes concurrent routing commits only for the callback's lifetime.
// Future reference-creating commits must independently validate Connection
// existence under this same boundary; inspection alone cannot prevent them.
// Lock order is routing publication, then dependent store publication.
func (s *Store) InspectLocked(ctx context.Context, inspect func(routing.Config) error) error {
	if s == nil {
		return ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if inspect == nil {
		return errors.New("routing inspection is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == storeClosed {
		return ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return inspect(s.snapshot.Load().Clone())
}
