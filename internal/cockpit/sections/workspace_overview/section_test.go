package workspace_overview

import (
	"reflect"
	"testing"

	tui "github.com/grindlemire/go-tui"
	workspace_delete "github.com/swobuforge/swobu/internal/cockpit/features/workspace_delete"
	workspace_edit "github.com/swobuforge/swobu/internal/cockpit/features/workspace_edit"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestSection_CopyClientBaseURLSetsState(t *testing.T) {
	section := Section(workspaceSectionModel())
	section.copyClientBaseURL()
	if !section.CopiedClientBaseURL.Get() {
		t.Fatal("copy row did not record local copy intent")
	}
}

func TestSection_RunOnceOpensWorkflow(t *testing.T) {
	section := Section(workspaceSectionModel())
	cmd := section.Model.RunCommands[0]
	section.openRun(cmd)
	if got, want := section.OpenRun.Get(), readmodel.RunCommandID("codex"); got != want {
		t.Fatalf("open run = %q, want %q", got, want)
	}
}

func TestSection_CopyClientBaseURLShowsVisibleFeedback(t *testing.T) {
	section := Section(workspaceSectionModel())
	section.copyClientBaseURL()

	if !section.CopiedClientBaseURL.Get() {
		t.Fatal("copy row did not record visible copy state")
	}
	rendered := testkit.RenderTrimmed(section.Render(nil), 100, 10)
	testkit.AssertVisual("copied_client_base_url").
		Fixture("testdata/workspace_overview/fixture/copied_client_base_url.txt").
		Viewport(100, 10).
		Now(t, rendered)
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
