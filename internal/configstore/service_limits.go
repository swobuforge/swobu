package configstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/swobuforge/swobu/internal/routing"
)

// SetServiceLimitsChecked publishes Workspace admission policy using the same
// Node transaction, ETag and effect-time authority as Service edits.
func (s *Store) SetServiceLimitsChecked(ctx context.Context, slug routing.WorkspaceSlug, limits routing.ServiceLimits, expected string, authorize func(context.Context) error) (routing.Workspace, error) {
	if s == nil {
		return routing.Workspace{}, ErrStoreClosed
	}
	if expected == "" || authorize == nil {
		return routing.Workspace{}, errors.New("Service limits require Workspace ETag and authority")
	}
	if err := limits.Validate(); err != nil {
		return routing.Workspace{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == storeClosed {
		return routing.Workspace{}, ErrStoreClosed
	}
	if s.readOnly {
		return routing.Workspace{}, ErrReadOnly
	}
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, err
	}
	current := s.snapshot.Load().Clone()
	workspace, exists := current.Workspace(slug)
	if !exists {
		return routing.Workspace{}, routing.ErrNotFound
	}
	if fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision()) != expected {
		return routing.Workspace{}, ErrStaleRevision
	}
	candidate, err := current.SetServiceLimits(slug, limits)
	if err != nil {
		return routing.Workspace{}, err
	}
	if err := authorize(ctx); err != nil {
		return routing.Workspace{}, err
	}
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, err
	}
	if workspace.ServiceLimits() == limits {
		return workspace, nil
	}
	candidate, err = stampWorkspaceRevisions(current, candidate)
	if err != nil {
		return routing.Workspace{}, err
	}
	candidate, err = s.publishLocked(current, candidate, nil, s.deletions, s.creations, s.displayNameAudits)
	if err != nil {
		return routing.Workspace{}, err
	}
	committed, _ := candidate.Workspace(slug)
	return committed, nil
}
