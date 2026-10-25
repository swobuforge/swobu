package configstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/swobuforge/swobu/internal/routing"
)

var ErrMCPMountReviewConflict = errors.New("MCP mount review changed")

// ApplyMCPMountChecked commits a reviewed mount at exactly one Workspace
// revision. The Connection reference remains pinned throughout publication,
// while guard rechecks Access, publication and the reviewed source identity.
// There is no replay journal: after an uncertain outcome, read the Workspace.
func (s *Store) ApplyMCPMountChecked(ctx context.Context, slug routing.WorkspaceSlug, alias string, mount routing.MCPMount, expected, connectionRevision string, authorize func(context.Context) error, guard func(routing.Workspace, func() error) error) (routing.Workspace, error) {
	if s == nil {
		return routing.Workspace{}, ErrStoreClosed
	}
	if expected == "" || connectionRevision == "" || authorize == nil || guard == nil {
		return routing.Workspace{}, errors.New("MCP mount requires reviewed revision, Connection revision and admission")
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
	if err := authorize(ctx); err != nil {
		return routing.Workspace{}, err
	}
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, err
	}

	current := s.snapshot.Load().Clone()
	workspace, exists := current.Workspace(slug)
	if !exists {
		return routing.Workspace{}, routing.ErrNotFound
	}
	if workspaceETag(workspace) != expected {
		return routing.Workspace{}, ErrStaleRevision
	}
	next, err := current.SetMCPMount(slug, alias, mount)
	if err != nil {
		return routing.Workspace{}, err
	}
	next, err = stampWorkspaceRevisions(current, next)
	if err != nil {
		return routing.Workspace{}, err
	}
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, err
	}

	var committed routing.Config
	publish := func() error {
		var err error
		var reviewed *ReviewedConnection
		if mount.Builtin == "" {
			reviewed = &ReviewedConnection{ID: mount.ConnectionID, Revision: connectionRevision}
		} else if mount.Builtin != routing.AdminMCPSource || connectionRevision != routing.AdminMCPRevision {
			return ErrMCPMountReviewConflict
		}
		committed, err = s.publishLocked(current, next, nil, s.deletions, s.creations, s.displayNameAudits, reviewed)
		return err
	}
	if err := guard(workspace, publish); err != nil {
		return routing.Workspace{}, err
	}
	result, exists := committed.Workspace(slug)
	if !exists {
		return routing.Workspace{}, errors.New("MCP mount publication did not produce a Workspace")
	}
	return result, nil
}

func workspaceETag(workspace routing.Workspace) string {
	return fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision())
}
