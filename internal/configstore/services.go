package configstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/swobuforge/swobu/internal/routing"
)

// SetWorkspaceServiceChecked is the only persistence path for the explicit
// Service lifecycle. It compares lifetime+revision under the same publication
// lock as route, MCP and Search mutation. A no-op does not rewrite YAML.
func (s *Store) SetWorkspaceServiceChecked(ctx context.Context, slug routing.WorkspaceSlug, kind routing.ServiceKind, enabled bool, expected string, authorize func(context.Context) error) (routing.Workspace, error) {
	if s == nil {
		return routing.Workspace{}, ErrStoreClosed
	}
	if expected == "" || authorize == nil {
		return routing.Workspace{}, errors.New("Service change requires exact ETag and trusted admission")
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
	candidate, err := current.SetServiceEnabled(slug, kind, enabled)
	if err != nil {
		return routing.Workspace{}, err
	}
	if err := authorize(ctx); err != nil {
		return routing.Workspace{}, err
	}
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, err
	}
	if workspace.HasService(kind) == enabled {
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
