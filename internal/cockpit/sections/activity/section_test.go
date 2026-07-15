package activity

import (
	"strings"
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
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

func TestFocusableRow_FocusUpdatesVisibleMarker(t *testing.T) {
	view := FocusableRow("latest", "codex -> gpt", "", func() {})
	row := collectFocusables(view.Render(nil))[0].(*tui.Element)

	row.Focus()
	if got, want := view.Render(nil).Children()[0].Text(), ">"; got != want {
		t.Fatalf("focused marker = %q, want %q", got, want)
	}

	row.Blur()
	if got := view.Render(nil).Children()[0].Text(); got != "" {
		t.Fatalf("blurred marker = %q, want empty", got)
	}
}

func TestSection_HidesZeroTokens(t *testing.T) {
	section := Section(activitySectionModel())
	section.OpenActivity.Set("req-1")

	rendered := testkit.RenderString(section.Render(nil), 100, 16)
	if strings.Contains(rendered, "tokens in") || strings.Contains(rendered, "tokens out") {
		t.Fatalf("zero token rows should be hidden:\n%s", rendered)
	}
}

func TestSection_ShowsNonZeroTokens(t *testing.T) {
	model := activitySectionModel()
	model.Activity.Latest.TokensIn = 1200
	model.Activity.Latest.TokensOut = 450
	section := Section(model)
	section.OpenActivity.Set("req-1")

	rendered := testkit.RenderString(section.Render(nil), 100, 16)
	for _, want := range []string{"tokens in", "1,200", "tokens out", "450"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("render missing %q:\n%s", want, rendered)
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
