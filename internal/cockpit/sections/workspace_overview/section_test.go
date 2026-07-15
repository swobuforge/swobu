package workspace_overview

import (
	"reflect"
	"testing"

	tui "github.com/grindlemire/go-tui"
	workspace_delete "github.com/swobuforge/swobu/internal/cockpit/features/workspace_delete"
	workspace_edit "github.com/swobuforge/swobu/internal/cockpit/features/workspace_edit"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestSection_FocusableWorkspaceRowsActivateLocally(t *testing.T) {
	section := Section(workspaceSectionModel())
	focusables := collectFocusables(section.Render(nil))
	if got, want := len(focusables), 4; got != want {
		t.Fatalf("workspace focusables = %d, want %d", got, want)
	}

	activate(t, focusables[1])
	if !section.CopiedClientBaseURL.Get() {
		t.Fatal("copy row did not record local copy intent")
	}

	activate(t, focusables[2])
	if got, want := section.OpenRun.Get(), readmodel.RunCommandID("codex"); got != want {
		t.Fatalf("open run = %q, want %q", got, want)
	}
}

func TestFocusableRow_FocusUpdatesVisibleMarker(t *testing.T) {
	row := collectFocusables(FocusableRow("client base URL", "http://127.0.0.1:7926/c/dev", "copy ↵", func() {}).Root)[0].(*tui.Element)

	row.Focus()
	if got, want := row.Children()[0].Text(), ">"; got != want {
		t.Fatalf("focused marker = %q, want %q", got, want)
	}

	row.Blur()
	if got := row.Children()[0].Text(); got != "" {
		t.Fatalf("blurred marker = %q, want empty", got)
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
		RunCommands: []readmodel.RunCommandReadModel{{
			ID:    "codex",
			Label: "Codex",
		}},
	}
}
