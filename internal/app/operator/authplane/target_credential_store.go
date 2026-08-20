package authplane

import (
	"context"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

// TargetCredentialStore binds an authentication result through the semantic
// workspace command service. It never reconstructs a target or workspace.
type TargetCredentialStore struct{ workspaces workspaces.Service }

func NewTargetCredentialStore(service workspaces.Service) TargetCredentialStore {
	return TargetCredentialStore{workspaces: service}
}

func (s TargetCredentialStore) SetCredential(ctx context.Context, subject CredentialSubject, credentialRef string) (string, error) {
	credentialRef = strings.TrimSpace(credentialRef)
	if credentialRef == "" {
		return "", fmt.Errorf("credential ref is required")
	}
	if strings.TrimSpace(subject.DraftSubject) != "" {
		return credentialRef, nil
	}
	if strings.TrimSpace(subject.Workspace) == "" || strings.TrimSpace(subject.Route) == "" || strings.TrimSpace(subject.TargetID) == "" {
		return "", fmt.Errorf("workspace, route, and target ID are required")
	}
	_, err := s.workspaces.ApplyCredentialReference(ctx, workspaces.ApplyCredentialReference{Workspace: subject.Workspace, Route: subject.Route, TargetID: subject.TargetID, Credential: credentialRef})
	if err != nil {
		return "", err
	}
	return credentialRef, nil
}
