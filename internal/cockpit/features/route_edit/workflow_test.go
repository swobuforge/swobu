package route_edit

import (
	"context"
	"errors"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestWorkflow_RenameExistingRouteCallsSaveRoute(t *testing.T) {
	var got ports.SaveRouteRequest
	var saved readmodel.RouteReadModel
	workflow := NewWorkflow("dev", routeEditRoute(false), func(_ context.Context, request ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
		got = request
		return readmodel.RouteReadModel{ID: "gpt-4.1-alt", ModelName: request.ModelName, Enabled: request.Enabled}, nil
	}, nil, func(route readmodel.RouteReadModel) {
		saved = route
	}, nil, nil)
	workflow.ModelName.Set("gpt-4.1-alt")

	workflow.Submit(context.Background())

	if got.WorkspaceID != "dev" || got.RouteID != "gpt-4.1" || got.ModelName != "gpt-4.1-alt" {
		t.Fatalf("save route request = %+v, want dev/gpt-4.1/gpt-4.1-alt", got)
	}
	if saved.ID != "gpt-4.1-alt" || workflow.Phase.Get() != PhaseViewing {
		t.Fatalf("saved=%#v phase=%v, want saved route and viewing phase", saved, workflow.Phase.Get())
	}
}

func TestWorkflow_SetDefaultCallsSaveRouteWithDefault(t *testing.T) {
	var got ports.SaveRouteRequest
	workflow := NewWorkflow("dev", routeEditRoute(false), func(_ context.Context, request ports.SaveRouteRequest) (readmodel.RouteReadModel, error) {
		got = request
		route := routeEditRoute(true)
		route.ModelName = request.ModelName
		return route, nil
	}, nil, nil, nil, nil)

	workflow.SetDefault(context.Background())

	if !got.Default {
		t.Fatalf("save route request = %+v, want Default=true", got)
	}
	if !workflow.Route.Default {
		t.Fatal("workflow route should be default after save")
	}
}

func TestWorkflow_DeleteRequiresConfirmation(t *testing.T) {
	deleteErr := errors.New("delete should wait for confirmation")
	workflow := NewWorkflow("dev", routeEditRoute(false), nil, func(context.Context, ports.DeleteRouteRequest) error {
		return deleteErr
	}, nil, nil, nil)

	workflow.ActivateDelete()
	if workflow.Phase.Get() != PhaseConfirmingDelete {
		t.Fatalf("phase after first delete = %v, want confirming delete", workflow.Phase.Get())
	}
	if workflow.Error.Get() != "" {
		t.Fatalf("error after arm = %q, want empty", workflow.Error.Get())
	}

	workflow.ActivateDelete()
	if workflow.Error.Get() != deleteErr.Error() {
		t.Fatalf("error after confirm = %q, want %q", workflow.Error.Get(), deleteErr.Error())
	}
}

func TestWorkflow_DeleteConfirmationVisual(t *testing.T) {
	workflow := NewWorkflow("dev", routeEditRoute(false), nil, nil, nil, nil, nil)
	workflow.ActivateDelete()

	rendered := testkit.RenderMountedTrimmed(t, workflow, 90, 6)
	testkit.AssertVisual("confirm_delete").
		Fixture("testdata/route_edit_workflow/fixture/confirm_delete.txt").
		Viewport(90, 6).
		Now(t, rendered)
}

func TestWorkflow_EditModeVisualMarker(t *testing.T) {
	workflow := NewWorkflow("dev", routeEditRoute(false), nil, nil, nil, nil, nil)
	workflow.ActivateName()

	rendered := testkit.RenderMountedTrimmed(t, workflow, 90, 6)
	testkit.AssertVisual("editing").
		Fixture("testdata/route_edit_workflow/fixture/editing.txt").
		Viewport(90, 6).
		Now(t, rendered)
}

func TestWorkflow_FocusMarkersReachModelDefaultAndDelete(t *testing.T) {
	workflow := NewWorkflow("dev", routeEditRoute(false), nil, nil, nil, nil, nil)
	h, err := testkit.NewHarness(workflow)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	h.Open()
	t.Cleanup(h.Close)

	testkit.AssertFocusVisible(t, h, h.FocusNext, "> model")
	testkit.AssertFocusVisible(t, h, func() {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	}, "> default")
	testkit.AssertFocusVisible(t, h, func() {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	}, "> delete")
}

func routeEditRoute(defaultRoute bool) readmodel.RouteReadModel {
	return readmodel.RouteReadModel{
		ID:        "gpt-4.1",
		ModelName: "gpt-4.1",
		Default:   defaultRoute,
		Enabled:   true,
		Targets: []readmodel.TargetReadModel{{
			ID:       "target-1",
			Provider: "openai_compatible",
			Model:    "gpt-4.1",
			Rank:     1,
			Weight:   1,
		}},
	}
}
