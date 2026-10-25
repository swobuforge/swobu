package workspaces

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/routing"
)

// SetRouteOrderChecked changes only precedence among existing model patterns.
// The routing aggregate rejects partial, duplicate, or invented pattern lists.
func (s Service) SetRouteOrderChecked(ctx context.Context, rawWorkspace string, patterns []string, expected string, authorize func(context.Context) error) (Workspace, error) {
	if s.store == nil {
		return Workspace{}, CommandError{Unavailable, "store unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(rawWorkspace)
	if err != nil || slug.String() != rawWorkspace {
		return Workspace{}, CommandError{InvalidArgument, "exact Workspace required"}
	}
	order := make([]routing.RouteName, len(patterns))
	for i, pattern := range patterns {
		order[i], err = routing.ParseRouteName(pattern)
		if err != nil {
			return Workspace{}, commandError(err)
		}
	}
	committed, err := s.store.SetRouteOrderChecked(ctx, slug, order, expected, authorize)
	if err != nil {
		if errors.Is(err, configstore.ErrStaleRevision) {
			return Workspace{}, err
		}
		return Workspace{}, commandError(err)
	}
	return projectWorkspace(committed), nil
}
