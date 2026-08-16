package routes

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestNamedDraftRouteMutationsStayLocal(t *testing.T) {
	model := readmodel.WorkspaceReadModel{ID: "+", Slug: "buildweek", State: readmodel.WorkspaceDraft}
	section := Section(model, nil)
	section.State.Routes = []readmodel.RouteReadModel{{ID: "chat", ModelName: "chat", Enabled: true}}
	var saveCalls, deleteCalls int
	section.SaveRoute = func(context.Context, ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
		saveCalls++
		return readmodel.RouteReadModel{}, nil
	}
	section.DeleteRoute = func(context.Context, ports.DeleteRouteRequest) error {
		deleteCalls++
		return nil
	}

	section.submitRouteName("chat", "assistant")
	if saveCalls != 0 || len(section.State.Routes) != 1 || section.State.Routes[0].ID != "assistant" {
		t.Fatalf("local rename: saveCalls=%d routes=%#v", saveCalls, section.State.Routes)
	}
	if err := section.confirmDeleteRoute("assistant"); err != nil {
		t.Fatal(err)
	}
	if deleteCalls != 0 || len(section.State.Routes) != 0 {
		t.Fatalf("local delete: deleteCalls=%d routes=%#v", deleteCalls, section.State.Routes)
	}
}

func TestConventionalFirstRouteMutationsStayLocal(t *testing.T) {
	model := readmodel.NewConventionalFirstWorkspace("http://127.0.0.1:7926/c/default", nil)
	section := Section(model, nil)
	section.State.Routes = []readmodel.RouteReadModel{{ID: "chat", ModelName: "chat", Enabled: true}}
	var saveCalls, deleteCalls int
	section.SaveRoute = func(context.Context, ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
		saveCalls++
		return readmodel.RouteReadModel{}, nil
	}
	section.DeleteRoute = func(context.Context, ports.DeleteRouteRequest) error {
		deleteCalls++
		return nil
	}

	section.submitRouteName("chat", "coding")
	if saveCalls != 0 || len(section.State.Routes) != 1 || section.State.Routes[0].ID != "coding" {
		t.Fatalf("local bootstrap rename: saveCalls=%d routes=%#v", saveCalls, section.State.Routes)
	}
	if err := section.confirmDeleteRoute("coding"); err != nil {
		t.Fatal(err)
	}
	if deleteCalls != 0 || len(section.State.Routes) != 0 {
		t.Fatalf("local bootstrap delete: deleteCalls=%d routes=%#v", deleteCalls, section.State.Routes)
	}
}

func TestFirstTargetResultPromotesNamedDraft(t *testing.T) {
	model := readmodel.WorkspaceReadModel{ID: "+", Slug: "buildweek", State: readmodel.WorkspaceDraft}
	section := Section(model, nil)
	var promoted readmodel.WorkspaceReadModel
	section.OnWorkspacePersisted = func(workspace readmodel.WorkspaceReadModel) { promoted = workspace }
	section.configureTargetConfigMounts()
	committed := readmodel.WorkspaceReadModel{ID: "buildweek", Slug: "buildweek", State: readmodel.WorkspaceExisting}
	section.TargetConfigs.Callbacks.OnCreated(ports.SaveTargetResult{
		Route:     readmodel.RouteReadModel{ID: "chat", ModelName: "chat"},
		Workspace: committed,
	})

	if promoted.ID != "buildweek" || promoted.IsDraft() {
		t.Fatalf("promoted workspace = %#v", promoted)
	}
}

func TestFirstTargetResultPromotesConventionalFirstWorkspace(t *testing.T) {
	model := readmodel.NewConventionalFirstWorkspace("http://127.0.0.1:7926/c/default", nil)
	section := Section(model, nil)
	var promoted readmodel.WorkspaceReadModel
	section.OnWorkspacePersisted = func(workspace readmodel.WorkspaceReadModel) { promoted = workspace }
	section.configureTargetConfigMounts()
	committed := readmodel.WorkspaceReadModel{
		ID: "default", Slug: "default", State: readmodel.WorkspaceExisting,
		WorkspaceURL: "http://127.0.0.1:7926/c/default",
	}
	section.TargetConfigs.Callbacks.OnCreated(ports.SaveTargetResult{
		Route:     readmodel.RouteReadModel{ID: "coding", ModelName: "coding"},
		Workspace: committed,
	})

	if promoted.ID != "default" || !promoted.IsPersisted() || promoted.WorkspaceURL != model.WorkspaceURL {
		t.Fatalf("promoted conventional workspace = %#v", promoted)
	}
}
