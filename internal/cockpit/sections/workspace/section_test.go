package workspace

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestSection_FocusableWorkspaceRowsActivateLocally(t *testing.T) {
	section := Section(workspaceSectionModel())
	focusables := collectFocusables(section.Render(nil))
	if got, want := len(focusables), 3; got != want {
		t.Fatalf("workspace focusables = %d, want %d", got, want)
	}

	activate(t, focusables[0])
	if !section.CopiedClientBaseURL.Get() {
		t.Fatal("copy row did not record local copy intent")
	}

	activate(t, focusables[1])
	if got, want := section.OpenRun.Get(), readmodel.RunCommandID("codex"); got != want {
		t.Fatalf("open run = %q, want %q", got, want)
	}

	activate(t, focusables[2])
	if !section.OpenWorkspaceEdit.Get() {
		t.Fatal("edit workspace row did not record local edit intent")
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
		View: readmodel.WorkspaceViewState{
			WorkspaceExpanded: true,
		},
		RunCommands: []readmodel.RunCommandReadModel{{
			ID:    "codex",
			Label: "Codex",
		}},
	}
}
