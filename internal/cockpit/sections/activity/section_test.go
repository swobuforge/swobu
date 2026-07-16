package activity

import (
	"context"
	"errors"
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestSection_ActivationOpensActivity(t *testing.T) {
	section := Section(activitySectionModel(), context.Background(), nil)
	section.openActivity(*section.ActivitySnapshot.Get().Latest)
	if got, want := section.OpenActivity.Get(), readmodel.ActivityID("req-1"); got != want {
		t.Fatalf("open activity = %q, want %q", got, want)
	}
}

func TestSection_ErrorRowShowsErrInspect(t *testing.T) {
	model := activitySectionModel()
	model.Activity.Latest.Error = true
	section := Section(model, context.Background(), nil)
	section.OpenActivity.Set("req-1")

	rendered := testkit.RenderMountedTrimmed(t, section, 100, 14)
	testkit.AssertNow(t, rendered, testkit.Text("err ↵").Exists())
}

func TestSection_HidesZeroTokens(t *testing.T) {
	section := Section(activitySectionModel(), context.Background(), nil)
	section.OpenActivity.Set("req-1")

	rendered := testkit.RenderMountedTrimmed(t, section, 100, 16)
	testkit.AssertNow(t, rendered, testkit.All(
		testkit.Not(testkit.Text("tokens in").Exists()),
		testkit.Not(testkit.Text("tokens out").Exists()),
	))
}

func TestSection_ShowsNonZeroTokens(t *testing.T) {
	model := activitySectionModel()
	model.Activity.Latest.TokensIn = 1200
	model.Activity.Latest.TokensOut = 450
	section := Section(model, context.Background(), nil)
	section.OpenActivity.Set("req-1")

	rendered := testkit.RenderMountedTrimmed(t, section, 100, 16)
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
	section := Section(model, context.Background(), nil)
	section.OpenActivity.Set("req-1")

	rendered := testkit.RenderMountedTrimmed(t, section, 100, 16)
	testkit.AssertVisual("expanded_latest").
		Fixture("testdata/activity_section/fixture/expanded_latest.txt").
		Viewport(100, 16).
		Now(t, rendered)
}

func TestSection_RefreshesLatestActivityOnExpand(t *testing.T) {
	stale := activitySectionModel()
	stale.Activity = readmodel.ActivityReadModel{}
	fresh := activitySectionModel()
	query := &fakeActivityQueries{activity: fresh.Activity}

	section := Section(stale, context.Background(), query)
	section.onToggle(true)

	if got, want := query.calls, 1; got != want {
		t.Fatalf("activity query calls = %d, want %d", got, want)
	}
	if latest, ok := section.ActivitySnapshot.Get().LatestRow(); !ok || latest.ID != "req-1" {
		t.Fatalf("latest activity after refresh = %#v, %v", latest, ok)
	}

	rendered := testkit.RenderMountedTrimmed(t, section, 100, 22)
	testkit.AssertNow(t, rendered, testkit.All(
		testkit.Not(testkit.Text("no requests yet").Exists()),
		testkit.Text("14:32:01").Exists(),
		testkit.Text("latest").Exists(),
	))
}

func TestSection_RefreshSkipsWhenCollapsed(t *testing.T) {
	query := &fakeActivityQueries{activity: activitySectionModel().Activity}
	section := Section(activitySectionModel(), context.Background(), query)
	section.Expanded.Set(false)

	if got := section.Refresh(); got {
		t.Fatal("Refresh() = true, want false")
	}

	if got, want := query.calls, 0; got != want {
		t.Fatalf("activity query calls = %d, want %d", got, want)
	}
}

func TestSection_RefreshAppliesVisibleChange(t *testing.T) {
	stale := activitySectionModel()
	stale.Activity = readmodel.ActivityReadModel{}
	fresh := activitySectionModel()
	query := &fakeActivityQueries{activity: fresh.Activity}
	section := Section(stale, context.Background(), query)

	if got := section.Refresh(); !got {
		t.Fatal("Refresh() = false, want true")
	}
	if got, want := query.calls, 1; got != want {
		t.Fatalf("activity query calls = %d, want %d", got, want)
	}
	if latest, ok := section.ActivitySnapshot.Get().LatestRow(); !ok || latest.ID != "req-1" {
		t.Fatalf("latest activity after refresh = %#v, %v", latest, ok)
	}
}

type fakeActivityQueries struct {
	calls    int
	activity readmodel.ActivityReadModel
	err      error
}

func (f *fakeActivityQueries) ListActivity(context.Context, ports.ListActivityRequest) (readmodel.ActivityReadModel, error) {
	f.calls++
	if f.err != nil {
		return readmodel.ActivityReadModel{}, f.err
	}
	return f.activity, nil
}

func (f *fakeActivityQueries) GetActivity(context.Context, readmodel.ActivityID) (readmodel.ActivityRowReadModel, error) {
	return readmodel.ActivityRowReadModel{}, errors.New("not implemented")
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
