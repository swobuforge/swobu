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
	summaries         []workspaceapi.WorkspaceSummary
	listCalls         int
	getResponses      []workspaceapi.Workspace
	getErrors         []error
	getCalls          int
	createResponse    workspaceapi.Workspace
	echoCreatedTarget bool
	created           workspaceapi.CreateWorkspace
	createCalls       int
	deleteErr         error
	deleteCalls       int
	replaceResponse   workspaceapi.Workspace
	replaced          workspaceapi.ReplaceRoute
	routeResponse     workspaceapi.RouteSpec
	getRouteCalls     int
}

func (s *workspaceClientStub) ListWorkspaces(context.Context) ([]workspaceapi.WorkspaceSummary, error) {
	s.listCalls++
	return append([]workspaceapi.WorkspaceSummary(nil), s.summaries...), nil
}

func (s *workspaceClientStub) DaemonVersion(context.Context) (string, error) { return "", nil }

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
	s.createCalls++
	s.created = cmd
	if s.echoCreatedTarget && len(s.createResponse.Routes) > 0 && len(s.createResponse.Routes[0].Tiers) > 0 && len(s.createResponse.Routes[0].Tiers[0].Targets) > 0 {
		s.createResponse.Slug = cmd.Slug
		s.createResponse.DefaultRoute = cmd.InitialRoute
		s.createResponse.Routes[0].Name = cmd.InitialRoute
		s.createResponse.Routes[0].Tiers[0].Targets[0].ID = cmd.Target.ID
		s.createResponse.Routes[0].Tiers[0].Targets[0].Model = cmd.Target.Model
		s.createResponse.Routes[0].Tiers[0].Targets[0].Protocol = cmd.Target.Protocol
		s.createResponse.Routes[0].Tiers[0].Targets[0].Connection = cmd.Target.Connection
	}
	return s.createResponse, nil
}

func (s *workspaceClientStub) DeleteWorkspace(context.Context, string) error {
	s.deleteCalls++
	return s.deleteErr
}

func (s *workspaceClientStub) ReplaceRoute(_ context.Context, cmd workspaceapi.ReplaceRoute) (workspaceapi.Workspace, error) {
	s.replaced = cmd
	return s.replaceResponse, nil
}

func (s *workspaceClientStub) GetRoute(context.Context, string, string) (workspaceapi.RouteSpec, error) {
	s.getRouteCalls++
	return s.routeResponse, nil
}

func (s *workspaceClientStub) Status(context.Context, string) (operatorclient.StatusProjection, error) {
	return operatorclient.StatusProjection{}, nil
}

func TestSaveTargetUsesAuthoritativeCommandResponse(t *testing.T) {
	before := workspaceView("old-model", "env:OLD")
	stub := &workspaceClientStub{
		getResponses: []workspaceapi.Workspace{before},
		routeResponse: workspaceapi.RouteSpec{Tiers: []workspaceapi.TierSpec{{Targets: []workspaceapi.TargetDraft{{
			ID: "a", Model: "old-model", Protocol: "responses", Connection: workspaceapi.StandardConnection("openai", "", "env:OLD"),
		}}}}},
		replaceResponse: workspaceapi.Workspace{Slug: "dev", DefaultRoute: "chat", Routes: []workspaceapi.Route{{
			Name: "chat",
			Tiers: []workspaceapi.Tier{{Targets: []workspaceapi.Target{{
				ID: "a", Model: "server-normalized-model", Protocol: "responses", Provider: "openai", Connection: workspaceapi.StandardConnection("openai", "", "env:COMMITTED"),
			}}}},
		}}},
	}
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
		Placement:   readmodel.PlacementOptionReadModel{Kind: readmodel.PlacementFallback},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.Model != "server-normalized-model" || result.Target.CredentialRef != "env:COMMITTED" {
		t.Fatalf("result target = %#v; want authoritative response", result.Target)
	}
	if stub.replaced.Route != "chat" || stub.replaced.Spec.Tiers[0].Targets[0].ID != "a" || stub.replaced.Spec.Tiers[0].Targets[0].Model != "client-model" {
		t.Fatalf("route replacement = %#v", stub.replaced)
	}
	if stub.getRouteCalls != 1 {
		t.Fatalf("editable route reads = %d, want canonical GetRoute once", stub.getRouteCalls)
	}
}

func TestSaveTargetPreservesOmittedDerivedProtocolFromCanonicalRouteSpec(t *testing.T) {
	workspace := workspaceapi.Workspace{Slug: "dev", DefaultRoute: "chat", Routes: []workspaceapi.Route{{Name: "chat"}}}
	chatGPT := workspaceapi.TargetDraft{ID: "chatgpt", Model: "gpt-5", Connection: workspaceapi.StandardConnection("chatgpt", "", "secretfile:chatgpt/default")}
	stub := &workspaceClientStub{
		getResponses:    []workspaceapi.Workspace{workspace},
		routeResponse:   workspaceapi.RouteSpec{Tiers: []workspaceapi.TierSpec{{Targets: []workspaceapi.TargetDraft{chatGPT}}}},
		replaceResponse: workspaceapi.Workspace{Slug: "dev", DefaultRoute: "chat", Routes: []workspaceapi.Route{{Name: "chat", Tiers: []workspaceapi.Tier{{Targets: []workspaceapi.Target{{ID: chatGPT.ID, Model: chatGPT.Model, Connection: chatGPT.Connection}}}}}}},
	}
	adapter := &LiveOperatorAdapter{client: stub, addr: "127.0.0.1:7926"}
	connection, err := chatGPT.Connection.RoutingConnection()
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID: "dev", RouteID: "chat", TargetID: "chatgpt", ModelID: "gpt-5", Connection: connection,
		Placement: readmodel.PlacementOptionReadModel{Kind: readmodel.PlacementFallback},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := stub.replaced.Spec.Tiers[0].Targets[0].Protocol; got != "" {
		t.Fatalf("derived protocol in Cockpit PUT = %q, want omitted", got)
	}
}

func TestRenameWorkspaceRejectsDraftNamingAtPersistenceBoundary(t *testing.T) {
	adapter := &LiveOperatorAdapter{client: &workspaceClientStub{}, addr: "127.0.0.1:7926"}
	if _, err := adapter.RenameWorkspace(context.Background(), ports.RenameWorkspaceRequest{ID: "+", Slug: "buildweek"}); err == nil {
		t.Fatal("draft naming crossed persistence boundary")
	}
}

func TestLoadCockpitProjectsConventionalFirstWorkspaceWithoutPersistence(t *testing.T) {
	stub := &workspaceClientStub{}
	adapter := &LiveOperatorAdapter{client: stub, addr: "127.0.0.1:9000"}

	for load := 1; load <= 2; load++ {
		model, err := adapter.LoadCockpit(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got := model.SelectedWorkspace; !got.IsBootstrap() || got.ID != "default" || got.Slug != "default" {
			t.Fatalf("load %d conventional projection = %#v", load, got)
		}
		if got := model.SelectedWorkspace.WorkspaceURL; got != "http://127.0.0.1:9000/c/default" {
			t.Fatalf("load %d endpoint = %q", load, got)
		}
		if len(model.Tabs) != 2 || model.Tabs[0].Kind != readmodel.WorkspaceTabBootstrap || !model.Tabs[0].Selected || model.Tabs[1].Kind != readmodel.WorkspaceTabHelp {
			t.Fatalf("load %d tabs = %#v, want [default, ?]", load, model.Tabs)
		}
	}
	if stub.listCalls != 2 || stub.getCalls != 0 || stub.created.Slug != "" {
		t.Fatalf("restart projection effects: list=%d get=%d create=%#v", stub.listCalls, stub.getCalls, stub.created)
	}
}

func TestLoadCockpitKeepsExistingInstallationsOrdinary(t *testing.T) {
	for _, tc := range []struct {
		name      string
		summaries []workspaceapi.WorkspaceSummary
		selected  workspaceapi.Workspace
	}{
		{name: "one", summaries: []workspaceapi.WorkspaceSummary{{Slug: "work"}}, selected: workspaceWithSlug("work")},
		{name: "many", summaries: []workspaceapi.WorkspaceSummary{{Slug: "work"}, {Slug: "personal"}}, selected: workspaceWithSlug("personal")},
		{name: "persisted default", summaries: []workspaceapi.WorkspaceSummary{{Slug: "default"}}, selected: workspaceWithSlug("default")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &workspaceClientStub{summaries: tc.summaries, getResponses: []workspaceapi.Workspace{tc.selected}}
			model, err := (&LiveOperatorAdapter{client: stub, addr: "127.0.0.1:7926"}).LoadCockpit(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !model.SelectedWorkspace.IsPersisted() || model.Tabs[0].Kind != readmodel.WorkspaceTabExisting {
				t.Fatalf("existing installation projected as onboarding: workspace=%#v tabs=%#v", model.SelectedWorkspace, model.Tabs)
			}
			if _, ok := draftTabIndexForTest(model.Tabs); !ok {
				t.Fatalf("existing installation tabs omit +: %#v", model.Tabs)
			}
		})
	}
}

func TestSaveFirstTargetAtomicallyPersistsNamedDraft(t *testing.T) {
	committed := workspaceView("gpt-4.1", "env:OPENAI_API_KEY")
	committed.Slug = "buildweek"
	stub := &workspaceClientStub{
		getErrors:         []error{&operatorclient.ResponseError{StatusCode: 404, Code: "NOT_FOUND"}},
		createResponse:    committed,
		echoCreatedTarget: true,
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

func TestSaveFirstTargetAtomicallyPersistsConventionalDefault(t *testing.T) {
	committed := workspaceWithSlug("default")
	stub := &workspaceClientStub{
		getErrors:         []error{&operatorclient.ResponseError{StatusCode: 404, Code: "NOT_FOUND"}},
		createResponse:    committed,
		echoCreatedTarget: true,
	}
	adapter := &LiveOperatorAdapter{client: stub, addr: "127.0.0.1:9000"}
	provider, _ := routing.ParseProvider("openai", func(raw string) bool { return raw == "openai" })
	connection, err := routing.NewStandardConnection(provider, "", "env:OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.SaveTarget(context.Background(), ports.SaveTargetRequest{
		WorkspaceID: "default", RouteID: "coding", ModelID: "gpt-5.3-codex", Protocol: "responses", Connection: connection,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.getCalls != 1 || stub.createCalls != 1 || stub.created.Slug != "default" || stub.created.InitialRoute != "coding" || stub.created.Target.Model != "gpt-5.3-codex" {
		t.Fatalf("atomic conventional create: get=%d create=%d command=%#v", stub.getCalls, stub.createCalls, stub.created)
	}
	if result.Workspace.WorkspaceURL != "http://127.0.0.1:9000/c/default" || !result.Workspace.IsPersisted() || len(result.Workspace.Routes) != 1 {
		t.Fatalf("committed projection = %#v", result.Workspace)
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

func workspaceWithSlug(slug string) workspaceapi.Workspace {
	workspace := workspaceView("gpt-5.3-codex", "env:OPENAI_API_KEY")
	workspace.Slug = slug
	workspace.DefaultRoute = "chat"
	return workspace
}

func draftTabIndexForTest(tabs []readmodel.WorkspaceTabReadModel) (int, bool) {
	for i, tab := range tabs {
		if tab.Kind == readmodel.WorkspaceTabDraft {
			return i, true
		}
	}
	return 0, false
}
