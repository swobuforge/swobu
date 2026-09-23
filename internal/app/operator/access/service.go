package access

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

type Service struct {
	state    *accessstate.Store
	routing  *configstore.Store
	bindings *routebindings.Coordinator
}

func NewService(state *accessstate.Store, routingStore *configstore.Store, bindings *routebindings.Coordinator) (Service, error) {
	if state == nil || routingStore == nil || bindings == nil {
		return Service{}, errors.New("access state, routing store, and binding coordinator are required")
	}
	return Service{state: state, routing: routingStore, bindings: bindings}, nil
}

type Credential struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	CreatedAt time.Time `json:"created_at"`
}
type Access struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	Status      string       `json:"status"`
	CreatedAt   time.Time    `json:"created_at,omitempty"`
	Credentials []Credential `json:"credentials"`
}
type IssueResult struct {
	Access     Access     `json:"access"`
	Credential Credential `json:"credential"`
	Token      string     `json:"token"`
}

func (s Service) List(context.Context) []Access {
	out := []Access{{ID: accessidentity.Local.String(), Name: "Local", Kind: "builtin", Status: "active", Credentials: []Credential{}}}
	for _, item := range s.state.List() {
		out = append(out, project(item))
	}
	sort.Slice(out[1:], func(i, j int) bool { return out[i+1].ID < out[j+1].ID })
	return out
}

func (s Service) Create(_ context.Context, name, workspaceRaw string) (IssueResult, error) {
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return IssueResult{}, err
	}
	unlock := s.bindings.Lock()
	defer unlock()
	if _, ok := s.routing.Config().Workspace(workspace); !ok {
		return IssueResult{}, fmt.Errorf("%w: workspace %q", routing.ErrNotFound, workspaceRaw)
	}
	identity, issued, err := s.state.Create(name, workspace)
	if err != nil {
		return IssueResult{}, err
	}
	return issueResult(identity, issued), nil
}

func (s Service) IssueCredential(_ context.Context, accessRaw, workspaceRaw string) (IssueResult, error) {
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return IssueResult{}, err
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return IssueResult{}, err
	}
	unlock := s.bindings.Lock()
	defer unlock()
	if _, ok := s.routing.Config().Workspace(workspace); !ok {
		return IssueResult{}, fmt.Errorf("%w: workspace %q", routing.ErrNotFound, workspaceRaw)
	}
	issued, err := s.state.IssueCredential(id, workspace)
	if err != nil {
		return IssueResult{}, err
	}
	var identity accessstate.Access
	for _, item := range s.state.List() {
		if item.ID == id {
			identity = item
			break
		}
	}
	return issueResult(identity, issued), nil
}

func (s Service) RevokeCredential(_ context.Context, accessRaw, credentialRaw string) error {
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return err
	}
	credentialID, err := accessstate.ParseCredentialID(credentialRaw)
	if err != nil {
		return err
	}
	_, err = s.state.RevokeCredential(id, credentialID)
	return err
}
func (s Service) Revoke(_ context.Context, accessRaw string) error {
	id, err := accessidentity.RestoreManaged(accessRaw)
	if err != nil {
		return err
	}
	_, err = s.state.RevokeAccess(id)
	return err
}

func project(item accessstate.Access) Access {
	status := "active"
	if item.RevokedAt != nil {
		status = "revoked"
	}
	out := Access{ID: item.ID.String(), Name: item.Name, Kind: "managed", Status: status, CreatedAt: item.CreatedAt, Credentials: []Credential{}}
	for _, credential := range item.Credentials {
		out.Credentials = append(out.Credentials, Credential{ID: credential.ID.String(), Workspace: credential.Workspace.String(), CreatedAt: credential.CreatedAt})
	}
	return out
}
func issueResult(identity accessstate.Access, issued accessstate.IssuedCredential) IssueResult {
	return IssueResult{Access: project(identity), Credential: Credential{ID: issued.Credential.ID.String(), Workspace: issued.Credential.Workspace.String(), CreatedAt: issued.Credential.CreatedAt}, Token: issued.Token}
}
