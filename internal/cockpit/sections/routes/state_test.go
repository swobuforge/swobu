package routes

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestRouteSectionState_ApplyRouteSavedRenamesExpandedRouteAndDefault(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	section.OpenRoute(section.State.Routes[1])

	saved := section.State.Routes[1]
	saved.ID = "local-renamed"
	saved.ModelName = "local-renamed"
	saved.Default = true

	section.saveRoute("local", saved)

	if got := section.State.ExpandedRoute.Get(); got != "local-renamed" {
		t.Fatalf("expanded route = %q, want local-renamed", got)
	}
	if section.State.Routes[0].Default {
		t.Fatal("previous default route should no longer be default")
	}
	if !section.State.Routes[1].Default {
		t.Fatal("saved route should be default")
	}
}

func TestRouteSectionState_ApplyTargetSavedUpdatesExpandedRouteAndClosesInlineState(t *testing.T) {
	section := Section(routeSectionModel(), nil)
	route := section.State.Routes[0]
	target := route.Targets[0]
	section.OpenTargetEditor(route, target)

	target.Provider = "typed-provider"
	section.saveTarget(route.ID, target)

	if got := section.State.Routes[0].Targets[0].Provider; got != "typed-provider" {
		t.Fatalf("target provider = %q, want typed-provider", got)
	}
	if got := section.State.OpenTarget.Get(); got != "" {
		t.Fatalf("open target = %q, want closed", got)
	}
}

func TestRouteSectionState_ApplyTargetDeletedRemovesTargetAndClosesInlineState(t *testing.T) {
	section := Section(routeSectionModel(), fakeRouteCommands{
		deleteTarget: func(context.Context, ports.DeleteTargetRequest) error { return nil },
	})
	route := section.State.Routes[0]
	section.OpenTargetEditor(route, route.Targets[0])

	if err := section.deleteTargetAndClose(route.ID, route.Targets[0].ID); err != nil {
		t.Fatalf("deleteTargetAndClose: %v", err)
	}

	if got := len(section.State.Routes[0].Targets); got != 1 {
		t.Fatalf("targets after delete = %d, want 1", got)
	}
	if section.State.Routes[0].Targets[0].ID == readmodel.TargetID("target-1") {
		t.Fatal("deleted target still present")
	}
	if got := section.State.OpenTarget.Get(); got != "" {
		t.Fatalf("open target = %q, want closed", got)
	}
}
