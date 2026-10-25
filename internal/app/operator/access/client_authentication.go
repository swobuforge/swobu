package access

import (
	"errors"
	"slices"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// ClientPermission is a read-only admission check for Connect's Models binding.
// It neither creates a Principal nor changes its Workspace permission.
func (s Service) ClientPermission(principal, workspace string) (string, error) {
	permission, err := s.ReadPermission(principal, workspace)
	if err != nil {
		return "", err
	}
	slug, err := routing.ParseWorkspaceSlug(workspace)
	if err != nil {
		return "", err
	}
	configured, ok := s.routing.Config().Workspace(slug)
	if !ok || !configured.HasService(routing.ServiceModels) {
		return "", routing.ErrServiceDisabled
	}
	if !permission.Allowed || permission.Permission == nil ||
		(permission.Permission.Services != nil && !slices.Contains(permission.Permission.Services, routing.ServiceModels)) {
		return "", errors.New("selected Principal requires current Workspace Models permission")
	}
	return permission.Revision, nil
}

// ClientCredential uses the data-plane authority for a non-billable check. It
// authenticates the exact bearer and rechecks current service permission; an ID
// or a valid token for another Principal/Workspace cannot stand in for it.
func (s Service) ClientCredential(principal, workspace, bearer string) (string, error) {
	resolution, err := s.state.Authenticate(bearer)
	if err != nil || resolution.AccessID.String() != principal || resolution.Workspace.String() != workspace {
		return "", accessstate.ErrUnauthorized
	}
	if _, err := s.state.ResolveCredentialForService(resolution.CredentialID, routing.ServiceModels); err != nil {
		return "", err
	}
	if _, err := s.ClientPermission(principal, workspace); err != nil {
		return "", err
	}
	return resolution.CredentialID.String(), nil
}

// ClientCredentialIssuance observes a retained issuance receipt without issuing
// anything. Recovery must distinguish no issuance from a lost one-shot secret.
func (s Service) ClientCredentialIssuance(principal, workspace, key string) (string, error) {
	return s.state.CredentialIssuance(principal, workspace, key)
}

// IssueClientCredentialChecked is confirmed Connect provisioning, using the
// existing receipt/secret owner and the original reviewed permission revision.
func (s Service) IssueClientCredentialChecked(principal, workspace, key, revision string, authorize func() error) (IssueResult, error) {
	id, err := accessidentity.RestoreManaged(principal)
	if err != nil {
		return IssueResult{}, err
	}
	slug, err := routing.ParseWorkspaceSlug(workspace)
	if err != nil {
		return IssueResult{}, err
	}
	unlock := s.bindings.Lock()
	defer unlock()
	configured, ok := s.routing.Config().Workspace(slug)
	if !ok || !configured.HasService(routing.ServiceModels) {
		return IssueResult{}, routing.ErrServiceDisabled
	}
	issued, replayed, err := s.state.IssueClientCredentialChecked(id, slug, key, revision, authorize)
	if err != nil {
		return IssueResult{}, err
	}
	return IssueResult{Credential: Credential{ID: issued.Credential.ID.String(), Workspace: slug.String(), CreatedAt: issued.Credential.CreatedAt}, Token: issued.Token, SecretNotRecoverable: replayed}, nil
}
