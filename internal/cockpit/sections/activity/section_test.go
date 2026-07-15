package activity

import (
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestSection_ActivationOpensActivity(t *testing.T) {
	section := Section(activitySectionModel())
	section.openActivity(*section.Workspace.Activity.Latest)
	if got, want := section.OpenActivity.Get(), readmodel.ActivityID("req-1"); got != want {
		t.Fatalf("open activity = %q, want %q", got, want)
	}
}

func TestSection_ErrorRowShowsErrInspect(t *testing.T) {
	model := activitySectionModel()
	model.Activity.Latest.Error = true
	section := Section(model)
	section.OpenActivity.Set("req-1")

	rendered := testkit.RenderTrimmed(section.Render(nil), 100, 14)
	testkit.AssertNow(t, rendered, testkit.Text("err ↵").Exists())
}

func TestSection_HidesZeroTokens(t *testing.T) {
	section := Section(activitySectionModel())
	section.OpenActivity.Set("req-1")

	rendered := testkit.RenderTrimmed(section.Render(nil), 100, 16)
	testkit.AssertNow(t, rendered, testkit.All(
		testkit.Not(testkit.Text("tokens in").Exists()),
		testkit.Not(testkit.Text("tokens out").Exists()),
	))
}

func TestSection_ShowsNonZeroTokens(t *testing.T) {
	model := activitySectionModel()
	model.Activity.Latest.TokensIn = 1200
	model.Activity.Latest.TokensOut = 450
	section := Section(model)
	section.OpenActivity.Set("req-1")

	rendered := testkit.RenderTrimmed(section.Render(nil), 100, 16)
	testkit.AssertNow(t, rendered, testkit.All(
		testkit.Text("tokens in").Exists(),
		testkit.Text("1,200").Exists(),
		testkit.Text("tokens out").Exists(),
		testkit.Text("450").Exists(),
	))
}

func TestSection_ExpandedLatestRowUsesIndentedDetails(t *testing.T) {
	model := activitySectionModel()
	model.Activity.Latest.ResolvedName = "resolved-name"
	model.Activity.Latest.Model = "gpt-4.1"
	model.Activity.Latest.Attempts = []readmodel.ActivityAttemptReadModel{
		{Label: "attempt", Rank: 1, Result: readmodel.ActivityAttemptSucceeded},
		{Label: "attempt", Rank: 2, Result: readmodel.ActivityAttemptFailed},
	}
	model.Activity.Latest.TokensIn = 1200
	model.Activity.Latest.TokensOut = 450
	section := Section(model)
	section.OpenActivity.Set("req-1")

	rendered := testkit.RenderTrimmed(section.Render(nil), 100, 16)
	testkit.AssertVisual("expanded_latest").
		Fixture("testdata/activity_section/fixture/expanded_latest.txt").
		Viewport(100, 16).
		Now(t, rendered)
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
