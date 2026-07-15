package workspace_edit

import (
	"context"
	"errors"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestWorkflow_OpenEditorSeedsExistingWorkspace(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{}, nil, nil)

	workflow.OpenEditor(readmodel.WorkspaceReadModel{
		ID:   "dev",
		Slug: "dev",
	})

	if workflow.Phase.Get() != PhaseEditing {
		t.Fatalf("phase = %v, want editing", workflow.Phase.Get())
	}
	if workflow.Mode.Get() != ModeEdit {
		t.Fatalf("mode = %v, want edit", workflow.Mode.Get())
	}
	if workflow.WorkspaceID.Get() != "dev" {
		t.Fatalf("workspace id = %q, want dev", workflow.WorkspaceID.Get())
	}
	if workflow.Slug.Get() != "dev" {
		t.Fatalf("slug = %q, want dev", workflow.Slug.Get())
	}
}

func TestWorkflow_OpenCreateStartsEmptyDraft(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{}, nil, nil)

	workflow.OpenCreate()

	if workflow.Phase.Get() != PhaseEditing {
		t.Fatalf("phase = %v, want editing", workflow.Phase.Get())
	}
	if workflow.Mode.Get() != ModeCreate {
		t.Fatalf("mode = %v, want create", workflow.Mode.Get())
	}
	if workflow.WorkspaceID.Get() != "" {
		t.Fatalf("workspace id = %q, want empty", workflow.WorkspaceID.Get())
	}
	if workflow.Slug.Get() != "" {
		t.Fatalf("slug = %q, want empty", workflow.Slug.Get())
	}
}

func TestWorkflow_SubmitValidationFailureStaysRecoverable(t *testing.T) {
	var called bool
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{}, func(context.Context, ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
		called = true
		return readmodel.WorkspaceReadModel{}, nil
	}, nil)
	workflow.OpenCreate()
	workflow.Slug.Set("Bad Slug")

	workflow.Submit(context.Background())

	if called {
		t.Fatal("save should not be called for invalid slug")
	}
	if workflow.Phase.Get() != PhaseFailed {
		t.Fatalf("phase = %v, want failed", workflow.Phase.Get())
	}
	if workflow.Error.Get() == "" {
		t.Fatal("validation error should be visible")
	}
}

func TestWorkflow_SubmitSuccessEmitsSavedWorkspaceAndCloses(t *testing.T) {
	var got ports.SaveWorkspaceRequest
	var saved readmodel.WorkspaceReadModel
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{}, func(ctx context.Context, request ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
		got = request
		return readmodel.WorkspaceReadModel{ID: "dev", Slug: request.Slug}, nil
	}, func(workspace readmodel.WorkspaceReadModel) {
		saved = workspace
	})
	workflow.OpenEditor(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"})
	workflow.Slug.Set("dev-2")

	workflow.Submit(context.Background())

	if got.ID != "dev" || got.Slug != "dev-2" {
		t.Fatalf("save request = %+v, want dev/dev-2", got)
	}
	if saved.Slug != "dev-2" {
		t.Fatalf("saved workspace = %+v, want slug dev-2", saved)
	}
	if workflow.Phase.Get() != PhaseViewing {
		t.Fatalf("phase = %v, want viewing", workflow.Phase.Get())
	}
}

func TestWorkflow_SubmitFailureLeavesDraftOpen(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{}, func(context.Context, ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
		return readmodel.WorkspaceReadModel{}, errors.New("disk full")
	}, nil)
	workflow.OpenCreate()
	workflow.Slug.Set("dev")

	workflow.Submit(context.Background())

	if workflow.Phase.Get() != PhaseFailed {
		t.Fatalf("phase = %v, want failed", workflow.Phase.Get())
	}
	if workflow.Slug.Get() != "dev" {
		t.Fatalf("slug = %q, want draft preserved", workflow.Slug.Get())
	}
	if workflow.Error.Get() != "disk full" {
		t.Fatalf("error = %q, want disk full", workflow.Error.Get())
	}
}

func TestWorkflow_BackClosesOpenWorkflow(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"}, nil, nil)
	workflow.OpenEditor(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"})
	workflow.Slug.Set("prod")

	if !workflow.Back() {
		t.Fatal("Back should consume open workflow")
	}
	if workflow.IsEditing() {
		t.Fatal("workflow should leave editing state")
	}
	if workflow.Slug.Get() != "dev" {
		t.Fatalf("slug after cancel = %q, want dev", workflow.Slug.Get())
	}
	if workflow.Back() {
		t.Fatal("Back should not consume viewing workflow")
	}
}

func TestWorkflow_KeyMapEscClosesFocusedWorkflow(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"}, nil, nil)
	workflow.OpenEditor(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"})
	workflow.Slug.Set("prod")

	pressBinding(t, workflow.KeyMap(), tui.KeyEscape)

	if workflow.IsEditing() {
		t.Fatal("Escape should leave editing state")
	}
	if workflow.Slug.Get() != "dev" {
		t.Fatalf("slug after Escape = %q, want dev", workflow.Slug.Get())
	}
}

func TestWorkflow_ActivateSlugRowEditsNoopsOrSubmits(t *testing.T) {
	var got ports.SaveWorkspaceRequest
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"}, func(ctx context.Context, request ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
		got = request
		return readmodel.WorkspaceReadModel{ID: "dev", Slug: request.Slug}, nil
	}, nil)

	workflow.Activate()
	if !workflow.IsEditing() {
		t.Fatal("first activation should edit existing slug")
	}

	workflow.Activate()
	if workflow.IsEditing() {
		t.Fatal("unchanged activation should finish editing")
	}
	if got.Slug != "" {
		t.Fatalf("unchanged activation should not save, got %+v", got)
	}

	workflow.Activate()
	workflow.Slug.Set("prod")
	workflow.Activate()
	if got.Slug != "prod" {
		t.Fatalf("changed activation saved slug %q, want prod", got.Slug)
	}
}

func TestWorkflow_RenderSlugLifecycleStatesInComponentLane(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"}, nil, nil)
	got := testkit.RenderString(workflow.Render(nil), 90, 4)
	assertContains(t, got, "slug", "dev", "edit")

	workflow.OpenEditor(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"})
	got = testkit.RenderString(workflow.Render(nil), 90, 8)
	assertContains(t, got, "slug", "dev", "save")

	workflow.Slug.Set("dev!")
	got = testkit.RenderString(workflow.Render(nil), 90, 9)
	assertContains(t, got, "invalid", "use lowercase letters, numbers, and hyphens")

	workflow.OpenCreate()
	got = testkit.RenderString(workflow.Render(nil), 90, 7)
	assertContains(t, got, "slug", "_", "create")
}

func TestWorkflow_ClientBaseURLPreviewDerivesFromSlug(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{ClientBaseURL: "http://127.0.0.1:7926/c/dev"}, nil, nil)
	workflow.OpenCreate()
	workflow.Slug.Set("staging")
	if got, want := workflow.ClientBaseURLPreview(), "http://127.0.0.1:7926/c/staging"; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}

	workflow.Slug.Set("staging!")
	if got, want := workflow.ClientBaseURLPreview(), "(derived from slug)"; got != want {
		t.Fatalf("invalid preview = %q, want %q", got, want)
	}
}

func assertContains(t *testing.T, got string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(got, value) {
			t.Fatalf("render should contain %q:\n%s", value, got)
		}
	}
}

func pressBinding(t *testing.T, keymap tui.KeyMap, key tui.Key) {
	t.Helper()
	for _, binding := range keymap {
		if binding.Pattern.Key == key {
			binding.Handler(tui.KeyEvent{Key: key})
			return
		}
	}
	t.Fatalf("keymap missing binding for %v", key)
}
