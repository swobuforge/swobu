package activity

import (
	"context"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestSection_RendersSuccessfulLatest(t *testing.T) {
	section := Section(successfulModel(), context.Background(), nil)
	rendered := testkit.RenderMountedTrimmed(t, section, 80, 3)
	testkit.AssertVisual("successful_latest").
		Fixture("testdata/activity_section/fixture/successful_latest.txt").
		Viewport(80, 3).
		Now(t, rendered)
}

func TestSection_RendersFailedLatest(t *testing.T) {
	m := successfulModel()
	m.Activity.Latest.ObservedAt = "14:35:10"
	m.Activity.Latest.Status = readmodel.ActivityFailed
	m.Activity.Latest.HTTPStatus = 500
	m.Activity.Latest.Duration = 312 * time.Millisecond
	section := Section(m, context.Background(), nil)
	rendered := testkit.RenderMountedTrimmed(t, section, 80, 3)
	testkit.AssertVisual("failed_latest").
		Fixture("testdata/activity_section/fixture/failed_latest.txt").
		Viewport(80, 3).
		Now(t, rendered)
}

func TestSection_RendersEmptyState(t *testing.T) {
	m := activityModel(readmodel.ActivityReadModel{})
	section := Section(m, context.Background(), nil)
	rendered := testkit.RenderMountedTrimmed(t, section, 80, 3)
	testkit.AssertVisual("empty").
		Fixture("testdata/activity_section/fixture/empty.txt").
		Viewport(80, 3).
		Now(t, rendered)
}

func TestSection_RendersDraftEmptyState(t *testing.T) {
	m := activityModel(readmodel.ActivityReadModel{})
	m.State = readmodel.WorkspaceDraft
	section := Section(m, context.Background(), nil)
	rendered := testkit.RenderMountedTrimmed(t, section, 80, 3)
	testkit.AssertVisual("draft").
		Fixture("testdata/activity_section/fixture/draft.txt").
		Viewport(80, 3).
		Now(t, rendered)
}

func TestSection_QueriesLatestActivity(t *testing.T) {
	stale := activityModel(readmodel.ActivityReadModel{})
	fresh := successfulModel()
	query := &fakeActivityQueries{activity: fresh.Activity}

	section := Section(stale, context.Background(), query)
	rendered := testkit.RenderMountedTrimmed(t, section, 80, 3)
	testkit.AssertNow(t, rendered, testkit.All(
		testkit.Text("14:32:01").Exists(),
		testkit.Text("codex").Exists(),
	))
}

func TestSection_ShowsNoRowWhenQueryErrors(t *testing.T) {
	stale := activityModel(readmodel.ActivityReadModel{})
	query := &fakeActivityQueries{err: context.DeadlineExceeded}

	section := Section(stale, context.Background(), query)
	rendered := testkit.RenderMountedTrimmed(t, section, 80, 3)
	testkit.AssertNow(t, rendered, testkit.Text("no requests yet").Exists())
}

func TestSection_QueriesWithLimitOne(t *testing.T) {
	stale := activityModel(readmodel.ActivityReadModel{})
	query := &fakeActivityQueries{activity: successfulModel().Activity}

	section := Section(stale, context.Background(), query)
	_ = testkit.RenderMountedTrimmed(t, section, 80, 3)

	if got, want := query.lastLimit, 1; got != want {
		t.Fatalf("query limit = %d, want %d", got, want)
	}
}

type fakeActivityQueries struct {
	activity  readmodel.ActivityReadModel
	err       error
	lastLimit int
}

func (f *fakeActivityQueries) ListActivity(_ context.Context, req ports.ListActivityRequest) (readmodel.ActivityReadModel, error) {
	f.lastLimit = req.Limit
	if f.err != nil {
		return readmodel.ActivityReadModel{}, f.err
	}
	return f.activity, nil
}

func successfulModel() readmodel.WorkspaceReadModel {
	row := readmodel.ActivityRowReadModel{
		ID:          "req-1",
		ObservedAt:  "14:32:01",
		ClientLabel: "codex",
		RouteID:     "gpt",
		Status:      readmodel.ActivitySucceeded,
		HTTPStatus:  200,
		Duration:    145 * time.Millisecond,
	}
	return activityModel(readmodel.ActivityReadModel{Latest: &row})
}

func activityModel(activity readmodel.ActivityReadModel) readmodel.WorkspaceReadModel {
	return readmodel.WorkspaceReadModel{
		ID:       "ws-dev",
		Slug:     "dev",
		State:    readmodel.WorkspaceExisting,
		Activity: activity,
	}
}
