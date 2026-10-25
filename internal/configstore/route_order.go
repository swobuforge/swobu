package configstore

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/swobuforge/swobu/internal/routing"
)

// SetRouteOrderChecked commits only a complete model-route priority permutation.
// Reordering does not touch target versions or any other
// Workspace Service. A stale preview cannot overwrite a concurrent change.
func (s *Store) SetRouteOrderChecked(ctx context.Context, slug routing.WorkspaceSlug, order []routing.RouteName, expected string, authorize func(context.Context) error) (routing.Workspace, error) {
	if s == nil {
		return routing.Workspace{}, ErrStoreClosed
	}
	if expected == "" || authorize == nil {
		return routing.Workspace{}, errors.New("route order requires Workspace ETag and trusted admission")
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
	candidate, err := current.SetRouteOrder(slug, order)
	if err != nil {
		return routing.Workspace{}, err
	}
	if err := authorize(ctx); err != nil {
		return routing.Workspace{}, err
	}
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, err
	}
	oldRoutes := workspace.Routes()
	if slices.EqualFunc(oldRoutes, order, func(route routing.Route, name routing.RouteName) bool { return route.Name() == name }) {
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
