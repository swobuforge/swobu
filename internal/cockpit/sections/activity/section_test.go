package activity

import (
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestSection_FocusableActivityRowActivatesLocally(t *testing.T) {
	section := Section(activitySectionModel())
	focusables := collectFocusables(section.Render(nil))
	if got, want := len(focusables), 1; got != want {
		t.Fatalf("activity focusables = %d, want %d", got, want)
	}

	activate(t, focusables[0])
	if got, want := section.OpenActivity.Get(), readmodel.ActivityID("req-1"); got != want {
		t.Fatalf("open activity = %q, want %q", got, want)
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

func activitySectionModel() readmodel.WorkspaceReadModel {
	row := readmodel.ActivityRowReadModel{
		ID:          "req-1",
		ObservedAt:  "14:32:01",
		ClientLabel: "codex",
		RouteID:     "gpt",
		Status:      readmodel.ActivitySucceeded,
		HTTPStatus:  200,
		Duration:    145 * time.Millisecond,
	}
	return readmodel.WorkspaceReadModel{
		Activity: readmodel.ActivityReadModel{
			Latest: &row,
		},
	}
}
