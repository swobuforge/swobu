package adapters

import (
	"context"
	"testing"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

type workspaceClientStub struct {
	operatorClient
	getResponses   []workspaceapi.Workspace
	getCalls       int
	updateResponse workspaceapi.Workspace
	updated        workspaceapi.UpdateTargetSettings
}

func (s *workspaceClientStub) GetWorkspace(context.Context, string) (workspaceapi.Workspace, error) {
	index := s.getCalls
	if index >= len(s.getResponses) {
		index = len(s.getResponses) - 1
	}
	s.getCalls++
	return s.getResponses[index], nil
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

	result, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID: "dev",
		RouteID:     "chat",
		TargetID:    "a",
		Draft: readmodel.TargetDraft{
			ProviderSpec: "openai", ProviderProtocol: "responses", ModelID: "client-model", CredentialRef: "env:CLIENT",
		},
		Placement: readmodel.PlacementOptionReadModel{Kind: readmodel.PlacementBalance, PeerTargetID: "a"},
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
				Connection: workspaceapi.Connection{OpenAI: &workspaceapi.CredentialConnection{Credential: credential}},
			}}}},
		}},
	}
}
