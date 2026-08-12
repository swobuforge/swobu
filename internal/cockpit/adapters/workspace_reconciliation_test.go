package adapters

import (
	"context"
	"testing"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/routing"
)

type workspaceClientStub struct {
	operatorClient
	getResponses   []workspaceapi.Workspace
	getErrors      []error
	getCalls       int
	createResponse workspaceapi.Workspace
	created        workspaceapi.CreateWorkspace
	deleteErr      error
	deleteCalls    int
	updateResponse workspaceapi.Workspace
	updated        workspaceapi.UpdateTargetSettings
}

func (s *workspaceClientStub) GetWorkspace(context.Context, string) (workspaceapi.Workspace, error) {
	index := s.getCalls
	if index < len(s.getErrors) && s.getErrors[index] != nil {
		s.getCalls++
		return workspaceapi.Workspace{}, s.getErrors[index]
	}
	if index >= len(s.getResponses) {
		index = len(s.getResponses) - 1
	}
	s.getCalls++
	return s.getResponses[index], nil
}

func (s *workspaceClientStub) CreateWorkspace(_ context.Context, cmd workspaceapi.CreateWorkspace) (workspaceapi.Workspace, error) {
	s.created = cmd
	return s.createResponse, nil
}

func (s *workspaceClientStub) DeleteWorkspace(context.Context, string) error {
	s.deleteCalls++
	return s.deleteErr
}

func (s *workspaceClientStub) UpdateTargetSettings(_ context.Context, cmd workspaceapi.UpdateTargetSettings) (workspaceapi.Workspace, error) {
	s.updated = cmd
	return s.updateResponse, nil
}

func (s *workspaceClientStub) Status(context.Context, string) (operatorclient.StatusProjection, error) {
	return operatorclient.StatusProjection{}, nil
}

func TestSaveTargetUsesAuthoritativeCommandResponse(t *testing.T) {
	before := workspaceView("old-model", "env:OLD")
	after := workspaceView("server-normalized-model", "env:COMMITTED")
	stub := &workspaceClientStub{getResponses: []workspaceapi.Workspace{before}, updateResponse: after}
	adapter := &LiveOperatorAdapter{client: stub, addr: "127.0.0.1:7926"}
	provider, _ := routing.ParseProvider("openai", func(raw string) bool { return raw == "openai" })
	connection, err := routing.NewStandardConnection(provider, "", "env:CLIENT")
	if err != nil {
		t.Fatal(err)
	}

	result, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID: "dev",
		RouteID:     "chat",
		TargetID:    "a",
		ModelID:     "client-model",
		Protocol:    "responses",
		Connection:  connection,
		Placement:   readmodel.PlacementOptionReadModel{Kind: readmodel.PlacementBalance, PeerTargetID: "a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.Model != "server-normalized-model" || result.Target.CredentialRef != "env:COMMITTED" {
		t.Fatalf("result target = %#v; want authoritative response", result.Target)
	}
	if stub.updated.TargetID != "a" || stub.updated.Target.Model != "client-model" {
		t.Fatalf("update command = %#v", stub.updated)
	}
}

func TestRenameWorkspaceRejectsDraftNamingAtPersistenceBoundary(t *testing.T) {
	adapter := &LiveOperatorAdapter{client: &workspaceClientStub{}, addr: "127.0.0.1:7926"}
	if _, err := adapter.RenameWorkspace(context.Background(), ports.RenameWorkspaceRequest{ID: "+", Slug: "buildweek"}); err == nil {
		t.Fatal("draft naming crossed persistence boundary")
	}
}

func TestSaveFirstTargetAtomicallyPersistsNamedDraft(t *testing.T) {
	committed := workspaceView("gpt-4.1", "env:OPENAI_API_KEY")
	committed.Slug = "buildweek"
	stub := &workspaceClientStub{
		getErrors:      []error{&operatorclient.ResponseError{StatusCode: 404, Code: "NOT_FOUND"}},
		createResponse: committed,
	}
	adapter := &LiveOperatorAdapter{client: stub, addr: "127.0.0.1:7926"}
	provider, _ := routing.ParseProvider("openai", func(raw string) bool { return raw == "openai" })
	connection, err := routing.NewStandardConnection(provider, "", "env:OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID: "buildweek", RouteID: "chat", TargetID: "a", ModelID: "gpt-4.1", Protocol: "responses", Connection: connection,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.created.Slug != "buildweek" || stub.created.InitialRoute != "chat" {
		t.Fatalf("atomic create command = %#v", stub.created)
	}
	if result.Workspace.ID != "buildweek" || result.Workspace.IsDraft() || len(result.Workspace.Routes) != 1 {
		t.Fatalf("persisted workspace result = %#v", result.Workspace)
	}
}

func TestDeletePersistedWorkspaceReconcilesNotFoundToAbsent(t *testing.T) {
	notFound := &operatorclient.ResponseError{StatusCode: 404, Code: "NOT_FOUND"}
	stub := &workspaceClientStub{deleteErr: notFound, getErrors: []error{notFound}}
	adapter := &LiveOperatorAdapter{client: stub, addr: "127.0.0.1:7926"}
	if err := adapter.DeleteWorkspace(context.Background(), ports.DeleteWorkspaceRequest{ID: "dev"}); err != nil {
		t.Fatalf("reconciled delete: %v", err)
	}
	if stub.deleteCalls != 1 || stub.getCalls != 1 {
		t.Fatalf("delete reconciliation calls: delete=%d get=%d", stub.deleteCalls, stub.getCalls)
	}
}

func TestDeletePersistedWorkspaceKeepsNotFoundFailureWhenObjectStillExists(t *testing.T) {
	notFound := &operatorclient.ResponseError{StatusCode: 404, Code: "NOT_FOUND"}
	stub := &workspaceClientStub{deleteErr: notFound, getResponses: []workspaceapi.Workspace{workspaceView("gpt-4.1", "env:KEY")}}
	adapter := &LiveOperatorAdapter{client: stub, addr: "127.0.0.1:7926"}
	if err := adapter.DeleteWorkspace(context.Background(), ports.DeleteWorkspaceRequest{ID: "dev"}); err == nil {
		t.Fatal("delete 404 was suppressed while workspace still existed")
	}
}

func TestLoadWorkspaceRefreshesFromDaemonInsteadOfRetainingStaleProjection(t *testing.T) {
	stub := &workspaceClientStub{getResponses: []workspaceapi.Workspace{
		workspaceView("first", "env:FIRST"),
		workspaceView("second", "env:SECOND"),
	}}
	adapter := &LiveOperatorAdapter{client: stub, addr: "127.0.0.1:7926"}

	first, err := adapter.LoadWorkspace(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.LoadWorkspace(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if first.Routes[0].Tiers[0].Targets[0].Model != "first" || second.Routes[0].Tiers[0].Targets[0].Model != "second" {
		t.Fatalf("refresh models = %q then %q", first.Routes[0].Tiers[0].Targets[0].Model, second.Routes[0].Tiers[0].Targets[0].Model)
	}
}

func workspaceView(model, credential string) workspaceapi.Workspace {
	return workspaceapi.Workspace{
		Slug: "dev", DefaultRoute: "chat",
		Routes: []workspaceapi.Route{{
			Name: "chat",
			Tiers: []workspaceapi.Tier{{Targets: []workspaceapi.Target{{
				ID: "a", Model: model, Protocol: "responses", Provider: "openai",
				Connection: workspaceapi.StandardConnection("openai", "", credential),
			}}}},
		}},
	}
}
