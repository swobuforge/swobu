package route_add

import (
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestDraft_OpenSeedsUniqueModelName(t *testing.T) {
	draft := NewDraft()

	draft.OpenFor([]readmodel.RouteReadModel{{ID: "route-new", ModelName: "route-new"}})

	if !draft.IsOpen() {
		t.Fatal("draft should be open")
	}
	if got, want := draft.ModelName.Get(), "route-new-2"; got != want {
		t.Fatalf("draft model = %q, want %q", got, want)
	}
}

func TestDraft_RouteUsesEditedModelNameAsIncompleteRoute(t *testing.T) {
	draft := NewDraft()
	draft.OpenFor(nil)
	draft.ModelName.Set("  custom-model  ")

	route := draft.Route(nil)

	if route.ID != "custom-model" || route.ModelName != "custom-model" {
		t.Fatalf("draft route = %#v, want custom-model identity", route)
	}
	if route.State != readmodel.RouteIncomplete || !route.Enabled {
		t.Fatalf("draft route state = %#v, want incomplete enabled route", route)
	}
}

func TestDraft_BackClosesOpenDraft(t *testing.T) {
	draft := NewDraft()
	draft.OpenFor(nil)

	if !draft.Back() {
		t.Fatal("Back should consume an open draft")
	}
	if draft.IsOpen() {
		t.Fatal("draft should be closed")
	}
	if draft.Back() {
		t.Fatal("Back should not consume a closed draft")
	}
}
