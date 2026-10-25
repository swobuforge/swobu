package configstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/swobuforge/swobu/internal/routing"
)

// PutModelRouteChecked publishes one complete route specification using the
// Workspace's single optimistic concurrency revision. A lost response can be
// reconciled by reading the Workspace; there is no parallel receipt journal.
func (s *Store) PutModelRouteChecked(ctx context.Context, slug routing.WorkspaceSlug, route routing.RouteName, desired routing.RouteSpec, expected string, authorize func(context.Context) error) (routing.Workspace, error) {
	if s == nil {
		return routing.Workspace{}, ErrStoreClosed
	}
	if expected == "" || authorize == nil {
		return routing.Workspace{}, errors.New("route update requires Workspace ETag and local admission")
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
	previous, found := current.Workspace(slug)
	if !found {
		return routing.Workspace{}, routing.ErrNotFound
	}
	if fmt.Sprintf("%s-%d", previous.ManagementIncarnation(), previous.ManagementRevision()) != expected {
		return routing.Workspace{}, ErrStaleRevision
	}
	next, err := current.PutRouteSpec(slug, route, desired)
	if err != nil {
		return routing.Workspace{}, err
	}
	if err := authorize(ctx); err != nil {
		return routing.Workspace{}, err
	}
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, err
	}
	candidate, _ := next.Workspace(slug)
	if reflect.DeepEqual(previous, candidate) {
		return previous, nil
	}
	next, err = stampWorkspaceRevisions(current, next)
	if err != nil {
		return routing.Workspace{}, err
	}
	committed, err := s.publishLocked(current, next, nil, s.deletions, s.creations, s.displayNameAudits)
	if err != nil {
		return routing.Workspace{}, err
	}
	workspace, _ := committed.Workspace(slug)
	return workspace, nil
}
