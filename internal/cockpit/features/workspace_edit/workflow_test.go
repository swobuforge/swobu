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

func TestWorkflow_UpdatePropsReseedsWhenWorkspaceIdentityChanges(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{
		ID:    "lab",
		Slug:  "lab",
		State: readmodel.WorkspaceExisting,
	}, nil, nil)

	workflow.OpenEditor(readmodel.WorkspaceReadModel{
		ID:    "lab",
		Slug:  "lab",
		State: readmodel.WorkspaceExisting,
	})
	workflow.Slug.Set("lab-2")

	workflow.UpdateProps(&Workflow{
		Workspace: readmodel.WorkspaceReadModel{
			ID:    "+",
			State: readmodel.WorkspaceDraft,
		},
	})

	if workflow.Mode.Get() != ModeCreate {
		t.Fatalf("mode = %v, want create", workflow.Mode.Get())
	}
	if workflow.Phase.Get() != PhaseEditing {
		t.Fatalf("phase = %v, want editing", workflow.Phase.Get())
	}
	if workflow.WorkspaceID.Get() != "+" {
		t.Fatalf("workspace id = %q, want +", workflow.WorkspaceID.Get())
	}
	if workflow.Slug.Get() != "" {
		t.Fatalf("slug = %q, want empty draft", workflow.Slug.Get())
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

func TestWorkflow_SubmitSlugBridgesEnterToSave(t *testing.T) {
	var saveCalled bool
	var saved readmodel.WorkspaceReadModel
	var request ports.SaveWorkspaceRequest
	// The mounted draft tab owns the "+" identifier; create submits must
	// preserve that draft ID so the live adapter stays on the create path.
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{ID: "+", State: readmodel.WorkspaceDraft}, func(ctx context.Context, got ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
		saveCalled = true
		request = got
		return readmodel.WorkspaceReadModel{ID: readmodel.WorkspaceID(got.Slug), Slug: got.Slug, State: readmodel.WorkspaceExisting}, nil
	}, func(workspace readmodel.WorkspaceReadModel) {
		saved = workspace
	})

	workflow.SubmitSlug("dev")

	if !saveCalled {
		t.Fatal("create submit should call the backend save seam")
	}
	if got, want := request.ID, readmodel.WorkspaceID("+"); got != want {
		t.Fatalf("save request id = %q, want +", got)
	}
	if workflow.Mode.Get() != ModeEdit {
		t.Fatalf("mode = %v, want edit after save", workflow.Mode.Get())
	}
	if saved.ID != "dev" || saved.Slug != "dev" || saved.State != readmodel.WorkspaceExisting {
		t.Fatalf("saved workspace = %#v, want existing dev", saved)
	}
}

func TestWorkflow_SubmitFailureLeavesDraftOpen(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{}, func(context.Context, ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
		return readmodel.WorkspaceReadModel{}, errors.New("disk full")
	}, nil)
	workflow.OpenEditor(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"})
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

func TestWorkflow_CreateSubmitFailureLeavesDraftOpen(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{}, func(context.Context, ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
		return readmodel.WorkspaceReadModel{}, errors.New("slug taken")
	}, nil)
	workflow.OpenCreate()
	workflow.Slug.Set("dev")

	workflow.Submit(context.Background())

	if workflow.Phase.Get() != PhaseFailed {
		t.Fatalf("phase = %v, want failed", workflow.Phase.Get())
	}
	if workflow.WorkspaceID.Get() != "" {
		t.Fatalf("workspace id = %q, want empty draft", workflow.WorkspaceID.Get())
	}
	if workflow.Error.Get() != "slug taken" {
		t.Fatalf("error = %q, want slug taken", workflow.Error.Get())
	}
}

func TestWorkflow_CreateSubmitFailureShowsSaveErrorInRender(t *testing.T) {
	// Save failures must render on the row itself; otherwise the PTY proof lane
	// cannot distinguish a rejected submit from a no-op Enter key.
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{}, func(context.Context, ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
		return readmodel.WorkspaceReadModel{}, errors.New("disk full")
	}, nil)
	workflow.OpenCreate()
	workflow.Slug.Set("dev")

	workflow.Submit(context.Background())

	if got, want := workflow.ActionLabel(), "invalid"; got != want {
		t.Fatalf("action label = %q, want %q", got, want)
	}

	rendered := testkit.RenderMountedTrimmed(t, workflow, 90, 9)
	testkit.AssertVisual("submit_failed").
		Fixture("testdata/workspace_edit_workflow/fixture/submit_failed.txt").
		Viewport(90, 9).
		Now(t, rendered)
}

func TestWorkflow_EditSubmitFailureShowsSaveErrorInRender(t *testing.T) {
	// Edit-mode save failures must render inline too; the shared visibleError
	// path works for both create and edit, but each mode needs its own fixture
	// regression because the row value and action semantics differ.
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{}, func(context.Context, ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
		return readmodel.WorkspaceReadModel{}, errors.New("name conflict")
	}, nil)
	workflow.OpenEditor(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"})
	workflow.Slug.Set("dev-2")

	workflow.Submit(context.Background())

	if got, want := workflow.ActionLabel(), "duplicate"; got != want {
		t.Fatalf("action label = %q, want %q", got, want)
	}
	if got, want := workflow.Slug.Get(), "dev-2"; got != want {
		t.Fatalf("slug = %q, want preserved dev-2", got)
	}

	rendered := testkit.RenderMountedTrimmed(t, workflow, 90, 9)
	testkit.AssertVisual("edit_submit_failed").
		Fixture("testdata/workspace_edit_workflow/fixture/edit_submit_failed.txt").
		Viewport(90, 9).
		Now(t, rendered)
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

func TestWorkflow_DraftCreateEscapeBacksOutAndClearsCursor(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{State: readmodel.WorkspaceDraft}, nil, nil)
	h, err := testkit.NewHarness(workflow)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'd'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'e'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'v'})

	if got := workflow.Slug.Get(); got != "dev" {
		t.Fatalf("slug before Escape = %q, want dev", got)
	}

	frame := h.Frame()
	if !strings.Contains(frame, "▌") {
		t.Fatalf("draft create frame should show the input cursor before Escape:\n%s", frame)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if workflow.IsEditing() {
		t.Fatal("Escape should leave draft create editing state")
	}
	if got := workflow.Slug.Get(); got != "" {
		t.Fatalf("slug after Escape = %q, want empty draft", got)
	}

	frame = h.Frame()
	if strings.Contains(frame, "▌") {
		t.Fatalf("draft create frame still contains a cursor after Escape:\n%s", frame)
	}
	if !strings.Contains(frame, "required") {
		t.Fatalf("draft create frame should return to the collapsed row after Escape:\n%s", frame)
	}

	workflow.Activate()

	if !workflow.IsEditing() {
		t.Fatal("activation should reopen draft create editing state")
	}

	frame = h.Frame()
	if !strings.Contains(frame, "▌") {
		t.Fatalf("draft create frame should restore the cursor after activation:\n%s", frame)
	}
}

func TestWorkflow_FocusedViewingRowKeepsMarkerAndFlipsToSaveOnEnter(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"}, nil, nil)
	h, err := testkit.NewHarness(workflow)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	h.FocusNext()

	frame := h.Frame()
	testkit.AssertFocusedFrame(t, frame, "> slug")
	if !strings.Contains(frame, "edit ↵") {
		t.Fatalf("frame missing view action before Enter:\n%s", frame)
	}

	workflow.Activate()

	frame = h.Frame()
	testkit.AssertFocusedFrame(t, frame, "> slug")
	if !strings.Contains(frame, "save ↵") {
		t.Fatalf("frame missing edit action after Enter:\n%s", frame)
	}
}

func TestWorkflow_DraftCreateAutoFocusesSlugInput(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{State: readmodel.WorkspaceDraft}, nil, nil)
	h, err := testkit.NewHarness(workflow)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	frame := h.Frame()
	testkit.AssertFocusedFrame(t, frame, "> slug")
	if !strings.Contains(frame, "▌") {
		t.Fatalf("frame missing slug cursor:\n%s", frame)
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: '?'})
	if got := workflow.Slug.Get(); got != "?" {
		t.Fatalf("slug after typing ? = %q, want ?", got)
	}
}

func TestWorkflow_DraftCreateEnterSubmitsWorkspace(t *testing.T) {
	var saveCalled bool
	var saved readmodel.WorkspaceReadModel
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{State: readmodel.WorkspaceDraft}, func(ctx context.Context, request ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
		saveCalled = true
		return readmodel.WorkspaceReadModel{ID: readmodel.WorkspaceID(request.Slug), Slug: request.Slug, State: readmodel.WorkspaceExisting}, nil
	}, func(workspace readmodel.WorkspaceReadModel) {
		saved = workspace
	})
	h, err := testkit.NewHarness(workflow)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'd'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'e'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'v'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if !saveCalled {
		t.Fatal("draft create submit should call the backend save seam")
	}
	if workflow.Mode.Get() != ModeEdit {
		t.Fatalf("mode = %v, want edit after submit", workflow.Mode.Get())
	}
	if saved.ID != "dev" || saved.Slug != "dev" || saved.State != readmodel.WorkspaceExisting {
		t.Fatalf("saved workspace = %#v, want existing dev", saved)
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
	rendered := testkit.RenderMountedTrimmed(t, workflow, 90, 9)
	testkit.AssertVisual("viewing").
		Fixture("testdata/workspace_edit_workflow/fixture/viewing.txt").
		Viewport(90, 9).
		Now(t, rendered)

	workflow.OpenEditor(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev"})
	rendered = testkit.RenderMountedTrimmed(t, workflow, 90, 9)
	testkit.AssertVisual("editing").
		Fixture("testdata/workspace_edit_workflow/fixture/editing.txt").
		Viewport(90, 9).
		Now(t, rendered)

	workflow.Slug.Set("dev!")
	rendered = testkit.RenderMountedTrimmed(t, workflow, 90, 9)
	testkit.AssertVisual("invalid").
		Fixture("testdata/workspace_edit_workflow/fixture/invalid.txt").
		Viewport(90, 9).
		Now(t, rendered)

	workflow.OpenCreate()
	rendered = testkit.RenderMountedTrimmed(t, workflow, 90, 9)
	testkit.AssertVisual("required").
		Fixture("testdata/workspace_edit_workflow/fixture/required.txt").
		Viewport(90, 9).
		Now(t, rendered)

	workflow.Slug.Set("dev")
	rendered = testkit.RenderMountedTrimmed(t, workflow, 90, 9)
	testkit.AssertVisual("create").
		Fixture("testdata/workspace_edit_workflow/fixture/create.txt").
		Viewport(90, 9).
		Now(t, rendered)

	workflow.Slug.Set("dev!")
	rendered = testkit.RenderMountedTrimmed(t, workflow, 90, 9)
	testkit.AssertVisual("invalid").
		Fixture("testdata/workspace_edit_workflow/fixture/invalid_create.txt").
		Viewport(90, 9).
		Now(t, rendered)

	workflow.Slug.Set("dev")
	workflow.Error.Set("name conflict")
	rendered = testkit.RenderMountedTrimmed(t, workflow, 90, 9)
	testkit.AssertVisual("duplicate").
		Fixture("testdata/workspace_edit_workflow/fixture/duplicate.txt").
		Viewport(90, 9).
		Now(t, rendered)
}

func TestWorkflow_ClientBaseURLPreviewDerivesFromSlug(t *testing.T) {
	workflow := NewWorkflow(readmodel.WorkspaceReadModel{ClientBaseURL: "http://127.0.0.1:7926/c/dev"}, nil, nil)
	workflow.OpenCreate()
	if got, want := workflow.ClientBaseURLPreview(), "after create"; got != want {
		t.Fatalf("empty preview = %q, want %q", got, want)
	}
	workflow.Slug.Set("staging")
	if got, want := workflow.ClientBaseURLPreview(), "http://127.0.0.1:7926/c/staging"; got != want {
		t.Fatalf("preview = %q, want %q", got, want)
	}

	workflow.Slug.Set("staging!")
	if got, want := workflow.ClientBaseURLPreview(), "after create"; got != want {
		t.Fatalf("invalid preview = %q, want %q", got, want)
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
