package workspaces

import (
	"context"
	"errors"
	"github.com/swobuforge/swobu/internal/configstore"

	"github.com/swobuforge/swobu/internal/routing"
)

// SetDisplayNameChecked changes presentation only. The routing owner compares
// exact durable lifetime/revision and admission at publication; slug, permission,
// credential and publication bindings are not renamed or revoked.
func (s Service) SetDisplayNameChecked(ctx context.Context, raw, name, expected string, authorize func(context.Context) error) (Workspace, error) {
	if s.store == nil {
		return Workspace{}, CommandError{Unavailable, "store unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		return Workspace{}, commandError(err)
	}
	workspace, err := s.store.SetWorkspaceDisplayNameChecked(ctx, slug, name, expected, authorize)
	if err != nil {
		if errors.Is(err, configstore.ErrStaleRevision) {
			return Workspace{}, err
		}
		// Admission belongs to the caller's trusted boundary and must preserve
		// its cause. Domain validation keeps the established command taxonomy.
		if errors.Is(err, routing.ErrInvalidConfig) || errors.Is(err, routing.ErrNotFound) {
			return Workspace{}, commandError(err)
		}
		return Workspace{}, err
	}
	return projectWorkspace(workspace), nil
}
