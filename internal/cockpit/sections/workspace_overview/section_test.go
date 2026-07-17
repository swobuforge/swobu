package workspace_overview

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	workspace_delete "github.com/swobuforge/swobu/internal/cockpit/features/workspace_delete"
	workspace_edit "github.com/swobuforge/swobu/internal/cockpit/features/workspace_edit"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

func TestSection_CopyEndpointSetsState(t *testing.T) {
	section := Section(workspaceSectionModel())
	section.copyEndpoint()
	if !section.CopiedEndpoint.Get() {
		t.Fatal("copy row did not record local copy intent")
	}
}

func TestSection_CopyEndpointShowsVisibleFeedback(t *testing.T) {
	section := Section(workspaceSectionModel())
	section.copyEndpoint()

	if !section.CopiedEndpoint.Get() {
		t.Fatal("copy row did not record visible copy state")
	}
	rendered := testkit.RenderMountedTrimmed(t, section, 100, 10)
	testkit.AssertVisual("copied_endpoint").
		Fixture("testdata/workspace_overview/fixture/copied_endpoint.txt").
		Viewport(100, 10).
		Now(t, rendered)
}

func TestSection_WorkspaceSavedResetsTransientState(t *testing.T) {
	section := Section(workspaceSectionModel())
	section.copyEndpoint()
	section.OpenDeleteConfirmation("dev")

	section.workspaceSaved(readmodel.WorkspaceReadModel{
		ID:   "dev",
		Slug: "dev-2",
	})

	if section.CopiedEndpoint.Get() {
		t.Fatal("copied state should reset after workspace save")
	}
	if got := section.PendingDeleteWorkspaceID.Get(); got != "" {
		t.Fatalf("pending delete workspace id after workspace save = %q, want empty", got)
	}
}

func TestSection_UpdatePropsKeepsPendingDeleteWorkspaceIDForSameWorkspace(t *testing.T) {
	section := Section(workspaceSectionModel())
	section.OpenDeleteConfirmation("dev")

	section.UpdateProps(&SectionView{
		Model: workspaceSectionModel(),
	})

	if got := section.PendingDeleteWorkspaceID.Get(); got != "dev" {
		t.Fatalf("pending delete workspace id after same-workspace refresh = %q, want dev", got)
	}
}

func TestSection_UsesStableFeatureMountKeys(t *testing.T) {
	section := Section(workspaceSectionModel())

	if got, want := workspaceEditKey(section), "workspace-edit:dev"; got != want {
		t.Fatalf("workspace edit key = %q, want %q", got, want)
	}
	if got, want := workspaceDeleteKey(section), "workspace-delete:dev"; got != want {
		t.Fatalf("workspace delete key = %q, want %q", got, want)
	}
}

func TestSection_DraftHeaderUsesLayoutGutter(t *testing.T) {
	section := Section(readmodel.WorkspaceReadModel{State: readmodel.WorkspaceDraft})
	rendered := testkit.RenderMountedString(t, section, 80, 6)

	if !strings.Contains(rendered, "  new workspace") {
		t.Fatalf("draft header should be indented by layout gutter:\n%s", rendered)
	}
	if strings.Contains(rendered, "\nnew workspace") {
		t.Fatalf("draft header should not rely on unstructured leading text:\n%s", rendered)
	}
}

func TestSection_DoesNotStorePersistentFeatureRefs(t *testing.T) {
	sectionType := reflect.TypeOf(SectionView{})
	for i := 0; i < sectionType.NumField(); i++ {
		field := sectionType.Field(i)
		if field.Type == reflect.TypeOf((*workspace_edit.Workflow)(nil)) {
			t.Fatalf("SectionView field %s stores *workspace_edit.Workflow", field.Name)
		}
		if field.Type == reflect.TypeOf((*workspace_delete.ConfirmationView)(nil)) {
			t.Fatalf("SectionView field %s stores *workspace_delete.ConfirmationView", field.Name)
		}
	}
}

func TestSection_FocusTraversal(t *testing.T) {
	section := Section(workspaceSectionModel())
	h := makeHarness(t, &workspaceSurfaceRoot{SectionView: section})
	defer h.Close()

	root := h.App().Root()
	if root == nil {
		t.Fatal("app.Root() returned nil")
	}
	focusables := collectFocusables(root)
	// header, endpoint, slug, delete = 4
	if got, want := len(focusables), 4; got != want {
		t.Fatalf("workspace focusables = %d, want %d", got, want)
	}
}

func TestSection_FocusMarkersRendered(t *testing.T) {
	section := Section(workspaceSectionModel())
	h := makeHarness(t, &workspaceSurfaceRoot{SectionView: section})
	defer h.Close()

	testkit.AssertFocusVisible(t, h, func() {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	}, "> ")
}

func TestSection_FocusMoveUpdatesMarker(t *testing.T) {
	section := Section(workspaceSectionModel())
	h := makeHarness(t, &workspaceSurfaceRoot{SectionView: section})
	defer h.Close()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	frame1 := h.Frame()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	frame2 := h.Frame()
	if frame1 == frame2 {
		t.Fatal("frames identical after focus move; expected different > row")
	}
}

func TestSection_EnterActivatesEndpointRow(t *testing.T) {
	section := Section(workspaceSectionModel())
	h := makeHarness(t, &workspaceSurfaceRoot{SectionView: section})
	defer h.Close()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	frameBefore := h.Frame()
	testkit.AssertFocusedFrame(t, frameBefore, "> endpoint")

	testkit.AssertFocusVisible(t, h, func() {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	}, "> ")

	if !section.CopiedEndpoint.Get() {
		t.Fatal("expected copy state set after Enter activation")
	}
}

func TestSection_SpaceActivatesEndpointRow(t *testing.T) {
	section := Section(workspaceSectionModel())
	h := makeHarness(t, &workspaceSurfaceRoot{SectionView: section})
	defer h.Close()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: ' '})

	if !section.CopiedEndpoint.Get() {
		t.Fatal("expected copy state set after Space activation")
	}
}

func TestSection_EnterActivatesSlugRow(t *testing.T) {
	section := Section(workspaceSectionModel())
	h := makeHarness(t, &workspaceSurfaceRoot{SectionView: section})
	defer h.Close()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	frameBefore := h.Frame()
	testkit.AssertFocusedFrame(t, frameBefore, "> slug")

	testkit.AssertFocusVisible(t, h, func() {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	}, "> ")
}

func TestSection_DeleteRowShowsMarkerWhenFocused(t *testing.T) {
	section := Section(workspaceSectionModel())
	h := makeHarness(t, &workspaceSurfaceRoot{SectionView: section})
	defer h.Close()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	frame := h.Frame()
	testkit.AssertFocusedFrame(t, frame, "> delete")
	if !strings.Contains(frame, "delete ↵") {
		t.Fatalf("expected delete action label, got:\n%s", frame)
	}
}

func TestSection_SpaceActivatesSlugRow(t *testing.T) {
	section := Section(workspaceSectionModel())
	h := makeHarness(t, &workspaceSurfaceRoot{SectionView: section})
	defer h.Close()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: ' '})

	frame := h.Frame()
	if !strings.Contains(frame, "save ↵") {
		t.Fatalf("expected slug edit mode after Space, got:\n%s", frame)
	}
}

func TestSection_DeleteConfirmation_CancelOnEscape(t *testing.T) {
	section := Section(workspaceSectionModel())
	section.DeleteWorkspace = func(_ context.Context, _ ports.DeleteWorkspaceRequest) error {
		return nil
	}
	h := makeHarness(t, &workspaceSurfaceRoot{SectionView: section})
	defer h.Close()

	// Navigate to the delete row.
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	// Arm the confirmation.
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	frameArmed := h.Frame()
	if !strings.Contains(frameArmed, "confirm ↵") {
		t.Fatalf("confirmation should show 'confirm ↵' after Enter, got:\n%s", frameArmed)
	}

	// Cancel with Escape.
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	frameAfter := h.Frame()
	if strings.Contains(frameAfter, "confirm ↵") {
		t.Fatalf("confirmation should be hidden after Escape, got:\n%s", frameAfter)
	}
	if !strings.Contains(frameAfter, "delete ↵") {
		t.Fatalf("row should show 'delete ↵' after cancel, got:\n%s", frameAfter)
	}
}

type workspaceSurfaceRoot struct {
	*SectionView
}

func (w *workspaceSurfaceRoot) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyUp, func(event tui.KeyEvent) {
			if app := event.App(); app != nil {
				app.FocusPrev()
			}
		}),
		tui.OnStop(tui.KeyDown, func(event tui.KeyEvent) {
			if app := event.App(); app != nil {
				app.FocusNext()
			}
		}),
		tui.OnStop(tui.KeyEnter, func(event tui.KeyEvent) {
			ui.ActivateCurrentSelection(event)
		}),
		tui.OnStop(tui.KeyRune, func(event tui.KeyEvent) {
			if event.Rune == ' ' {
				ui.ActivateCurrentSelection(event)
			}
		}),
	}
}

func makeHarness(t *testing.T, root tui.Component) *testkit.MockAppHarness {
	t.Helper()
	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	h.Open()
	return h
}

func collectFocusables(root *tui.Element) []tui.Focusable {
	var focusables []tui.Focusable
	root.WalkFocusables(func(f tui.Focusable) {
		focusables = append(focusables, f)
	})
	return focusables
}

func activate(t *testing.T, focusable tui.Focusable) {
	t.Helper()
	el, ok := focusable.(*tui.Element)
	if !ok {
		t.Fatalf("focusable is %T, want *tui.Element", focusable)
	}
	if !el.Activate() {
		t.Fatal("focusable did not handle activation")
	}
}

func workspaceSectionModel() readmodel.WorkspaceReadModel {
	return readmodel.WorkspaceReadModel{
		Slug:          "dev",
		State:         readmodel.WorkspaceExisting,
		ClientBaseURL: "http://127.0.0.1:7926/c/dev",
	}
}
