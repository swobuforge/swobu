package workspaces

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/routing"
)

// PutModelRouteChecked accepts the complete Models route as one revision-bound
// operation, including first-route creation. It cannot infer target protocols.
func (service Service) PutModelRouteChecked(ctx context.Context, command ReplaceRoute, expected string, authorize func(context.Context) error) (Workspace, error) {
	if service.store == nil {
		return Workspace{}, CommandError{Unavailable, "store unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(command.Workspace)
	if err != nil || slug.String() != command.Workspace {
		return Workspace{}, CommandError{InvalidArgument, "exact Workspace required"}
	}
	name, err := routing.ParseRouteName(command.Route)
	if err != nil || name.String() != command.Route {
		return Workspace{}, CommandError{InvalidArgument, "exact Route required"}
	}
	desired, err := command.Spec.routingSpec()
	if err != nil {
		return Workspace{}, commandError(err)
	}
	workspace, err := service.store.PutModelRouteChecked(ctx, slug, name, desired, expected, authorize)
	if err != nil {
		if errors.Is(err, configstore.ErrStaleRevision) {
			return Workspace{}, err
		}
		if errors.Is(err, routing.ErrInvalidConfig) || errors.Is(err, routing.ErrNotFound) || errors.Is(err, routing.ErrConflict) {
			return Workspace{}, commandError(err)
		}
		return Workspace{}, err
	}
	return projectWorkspace(workspace), nil
}
