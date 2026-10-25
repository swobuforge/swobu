package workspaces

import (
	"context"
	"errors"
	"fmt"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/routing"
)

// ChangeSearchRouteChecked is the sole Search desired-state mutation. It validates
// every referenced Connection and CASes the complete chain at Workspace revision.
func (s Service) ChangeSearchRouteChecked(ctx context.Context, rawWorkspace, connectionID string, fallback []string, expected string, authorize func(context.Context) error) (Workspace, error) {
	if s.store == nil {
		return Workspace{}, CommandError{Unavailable, "store unavailable"}
	}
	if expected == "" || authorize == nil {
		return Workspace{}, CommandError{InvalidArgument, "Workspace ETag and trusted admission required"}
	}
	slug, err := routing.ParseWorkspaceSlug(rawWorkspace)
	if err != nil || slug.String() != rawWorkspace {
		return Workspace{}, CommandError{InvalidArgument, "exact Workspace required"}
	}
	for _, id := range append([]string{connectionID}, fallback...) {
		if id == "" {
			continue
		}
		if err := s.validateConnectionReference(id, connections.Search); err != nil {
			return Workspace{}, err
		}
	}
	next, err := s.store.Update(ctx, func(current routing.Config) (routing.Config, error) {
		workspace, ok := current.Workspace(slug)
		if !ok {
			return routing.Config{}, routing.ErrNotFound
		}
		etag := fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision())
		if etag != expected {
			return routing.Config{}, configstore.ErrStaleRevision
		}
		if err := authorize(ctx); err != nil {
			return routing.Config{}, err
		}
		if err := ctx.Err(); err != nil {
			return routing.Config{}, err
		}
		return current.SetSearchRoute(slug, connectionID, fallback)
	})
	if err != nil {
		if errors.Is(err, configstore.ErrStaleRevision) {
			return Workspace{}, err
		}
		return Workspace{}, commandError(err)
	}
	committed, ok := next.Workspace(slug)
	if !ok {
		return Workspace{}, CommandError{Internal, "committed Workspace missing"}
	}
	return projectWorkspace(committed), nil
}

// DeleteMCPMountChecked removes an allowed tool catalog only at the reviewed
// Workspace revision. No grant or upstream credential is deleted.
func (s Service) DeleteMCPMountChecked(ctx context.Context, rawWorkspace, alias, expected string, authorize func(context.Context) error) (Workspace, error) {
	if s.store == nil {
		return Workspace{}, CommandError{Unavailable, "store unavailable"}
	}
	if expected == "" || authorize == nil {
		return Workspace{}, CommandError{InvalidArgument, "Workspace ETag and trusted admission required"}
	}
	slug, err := routing.ParseWorkspaceSlug(rawWorkspace)
	if err != nil || slug.String() != rawWorkspace {
		return Workspace{}, CommandError{InvalidArgument, "exact Workspace required"}
	}
	next, err := s.store.Update(ctx, func(current routing.Config) (routing.Config, error) {
		workspace, ok := current.Workspace(slug)
		if !ok {
			return routing.Config{}, routing.ErrNotFound
		}
		if fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision()) != expected {
			return routing.Config{}, configstore.ErrStaleRevision
		}
		if err := authorize(ctx); err != nil {
			return routing.Config{}, err
		}
		if err := ctx.Err(); err != nil {
			return routing.Config{}, err
		}
		return current.DeleteMCPMount(slug, alias)
	})
	if err != nil {
		if errors.Is(err, configstore.ErrStaleRevision) {
			return Workspace{}, err
		}
		return Workspace{}, commandError(err)
	}
	committed, ok := next.Workspace(slug)
	if !ok {
		return Workspace{}, CommandError{Internal, "committed Workspace missing"}
	}
	return projectWorkspace(committed), nil
}

// DeleteModelRouteChecked removes a client-facing Models route only against the
// current Workspace revision. Deleting the last route leaves Models
// enabled and empty. Durable target generation history is retained by routing.
func (s Service) DeleteModelRouteChecked(ctx context.Context, rawWorkspace, rawRoute, expected string, authorize func(context.Context) error) (Workspace, error) {
	if s.store == nil {
		return Workspace{}, CommandError{Unavailable, "store unavailable"}
	}
	if expected == "" || authorize == nil {
		return Workspace{}, CommandError{InvalidArgument, "Workspace ETag and trusted admission required"}
	}
	slug, err := routing.ParseWorkspaceSlug(rawWorkspace)
	if err != nil || slug.String() != rawWorkspace {
		return Workspace{}, CommandError{InvalidArgument, "exact Workspace required"}
	}
	name, err := routing.ParseRouteName(rawRoute)
	if err != nil || name.String() != rawRoute {
		return Workspace{}, CommandError{InvalidArgument, "exact Route required"}
	}
	var admissionErr error
	next, err := s.store.UpdatePrepared(ctx, func(current routing.Config) (routing.Config, error) {
		workspace, ok := current.Workspace(slug)
		if !ok {
			return routing.Config{}, routing.ErrNotFound
		}
		if fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision()) != expected {
			return routing.Config{}, configstore.ErrStaleRevision
		}
		if admissionErr = authorize(ctx); admissionErr != nil {
			return routing.Config{}, admissionErr
		}
		if err := ctx.Err(); err != nil {
			return routing.Config{}, err
		}
		return current.DeleteRoute(slug, name)
	}, nil)
	if err != nil {
		if admissionErr != nil {
			return Workspace{}, admissionErr
		}
		if errors.Is(err, configstore.ErrStaleRevision) {
			return Workspace{}, err
		}
		return Workspace{}, commandError(err)
	}
	committed, ok := next.Workspace(slug)
	if !ok {
		return Workspace{}, CommandError{Internal, "committed Workspace missing"}
	}
	return projectWorkspace(committed), nil
}
