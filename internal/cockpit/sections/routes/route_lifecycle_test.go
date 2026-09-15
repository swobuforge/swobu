package routes

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestPersistedRouteRenameRekeysOpenAddTargetBeforeRefresh(t *testing.T) {
	old := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Enabled: true}
	newRoute := readmodel.RouteReadModel{ID: "assistant", ModelName: "assistant", Enabled: true}
	section := Section(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{old}}, nil)
	section.State.ExpandedRoute.Set(old.ID)
	section.AddTarget(old)
	mount := section.TargetConfigs.CachedAdd(old.ID)
	mount.Draft.Set(readmodel.TargetDraft{ProviderSpec: "openai", ModelID: "unsaved-model", CredentialRef: "env:UNSAVED"})
	section.State.AddTargetRoute.Set(old.ID)
	section.SaveRoute = func(context.Context, ports.SaveRouteRequest) (ports.RouteMutationResult, error) {
		return ports.RouteMutationResult{Route: newRoute, Workspace: readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{newRoute}}}, nil
	}

	section.submitRouteName(old.ID, newRoute.ModelName)

	if section.State.ExpandedRoute.Get() != newRoute.ID || section.State.AddTargetRoute.Get() != newRoute.ID {
		t.Fatalf("rename selectors = expanded %q add %q", section.State.ExpandedRoute.Get(), section.State.AddTargetRoute.Get())
	}
	if got := section.TargetConfigs.CachedAdd(newRoute.ID); got != mount {
		t.Fatal("rename replaced open add-target component")
	}
	if got := section.TargetConfigs.CachedAdd(newRoute.ID).Draft.Get().ModelID; got != "unsaved-model" {
		t.Fatalf("draft model = %q, want unsaved-model", got)
	}
}

func TestPersistedRouteDeleteCapturesFocusBeforeCommittedReplacement(t *testing.T) {
	routes := []readmodel.RouteReadModel{{ID: "first", ModelName: "first"}, {ID: "chat", ModelName: "chat"}, {ID: "last", ModelName: "last"}}
	section := Section(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: routes}, nil)
	section.DeleteRoute = func(context.Context, ports.DeleteRouteRequest) (ports.RouteMutationResult, error) {
		return ports.RouteMutationResult{Workspace: readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{routes[0], routes[2]}}}, nil
	}
	if err := section.confirmDeleteRoute("chat"); err != nil {
		t.Fatal(err)
	}
	if got := section.State.FocusRoute.Get(); got != "last" {
		t.Fatalf("focus route = %q, want last", got)
	}
}

func TestPersistedRouteRenameRekeysOpenEditTargetBeforeRefresh(t *testing.T) {
	old := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Enabled: true, Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{{ID: "target", Model: "old"}}}}}
	newRoute := old
	newRoute.ID, newRoute.ModelName = "assistant", "assistant"
	section := Section(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{old}}, nil)
	mount := section.TargetConfigs.Edit(old, old.Tiers[0].Targets[0])
	mount.Draft.Set(readmodel.TargetDraft{ProviderSpec: "openai", ModelID: "unsaved-edit"})
	section.SaveRoute = func(context.Context, ports.SaveRouteRequest) (ports.RouteMutationResult, error) {
		return ports.RouteMutationResult{Route: newRoute, Workspace: readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{newRoute}}}, nil
	}
	section.submitRouteName(old.ID, newRoute.ModelName)
	if got := section.TargetConfigs.Edit(newRoute, newRoute.Tiers[0].Targets[0]); got != mount {
		t.Fatal("rename replaced open edit-target component")
	}
	if got := mount.Draft.Get().ModelID; got != "unsaved-edit" {
		t.Fatalf("edit draft model = %q, want unsaved-edit", got)
	}
}

func TestTargetConfigCancelStartsFreshInteractionLifetime(t *testing.T) {
	route := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Enabled: true}
	section := Section(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{route}}, nil)
	section.AddTarget(route)
	first := section.TargetConfigs.CachedAdd(route.ID)
	first.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.ProviderSpec = "unsaved-provider"
		return d
	})
	first.Close()
	section.AddTarget(route)
	second := section.TargetConfigs.CachedAdd(route.ID)
	if second == first {
		t.Fatal("canceled create reused its target config instance")
	}
	if got := second.Draft.Get().ProviderSpec; got != "" {
		t.Fatalf("reopened create retained provider %q", got)
	}
}

func TestTargetEditCancelReseedsLatestDurableTarget(t *testing.T) {
	target := readmodel.TargetReadModel{ID: "one", Provider: "openai", Model: "gpt-old", CredentialRef: "env:OPENAI_API_KEY"}
	route := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Enabled: true, Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	section := Section(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{route}}, nil)
	section.OpenTargetEditor(route, target)
	first := section.TargetConfigs.Edit(route, target)
	first.SelectedModel.Set(readmodel.ModelAuthoringOptionReadModel{ModelName: "unsaved"})
	first.Close()

	target.Model = "gpt-new"
	route.Tiers[0].Targets[0] = target
	section.State.Routes[0] = route
	section.OpenTargetEditor(route, target)
	second := section.TargetConfigs.Edit(route, target)
	if second == first {
		t.Fatal("canceled edit reused its target config instance")
	}
	if got := second.SelectedModel.Get().ModelName; got != "gpt-new" {
		t.Fatalf("reopened edit model = %q, want latest durable value", got)
	}
}
