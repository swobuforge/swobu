package workspaces

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/routing"
)

// SetServiceLimitsChecked changes Workspace operational admission policy.
// It never touches the independent durable spending-policy ledger.
func (s Service) SetServiceLimitsChecked(ctx context.Context, raw string, policy routing.ServiceLimits, expected string, authorize func(context.Context) error) (Workspace, error) {
	if s.store == nil {
		return Workspace{}, CommandError{Unavailable, "Workspace store unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		return Workspace{}, commandError(err)
	}
	if expected == "" || authorize == nil {
		return Workspace{}, CommandError{InvalidArgument, "strong Workspace ETag and authority required"}
	}
	committed, err := s.store.SetServiceLimitsChecked(ctx, slug, policy, expected, authorize)
	if err != nil {
		if errors.Is(err, configstore.ErrStaleRevision) {
			return Workspace{}, err
		}
		return Workspace{}, commandError(err)
	}
	return projectWorkspace(committed), nil
}
