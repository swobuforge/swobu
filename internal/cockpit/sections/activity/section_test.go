package activity

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestSection_RendersSuccessfulLatest(t *testing.T) {
	m := successfulModel()
	m.Activity.Latest.ProviderModel = "gpt-4.1-with-a-long-evidence-label"
	m.Activity.Latest.ClientLabel = "claude-cli/2.1.204"
	section := Section(m, context.Background(), nil)
	for _, tc := range []struct {
		name    string
		width   int
		fixture string
	}{
		{name: "narrow_60", width: 60, fixture: "successful_narrow_60.txt"},
		{name: "standard_80", width: 80, fixture: "successful_standard_80.txt"},
		{name: "wide_100", width: 100, fixture: "successful_wide_100.txt"},
		{name: "wide_120", width: 120, fixture: "successful_wide_120.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rendered := testkit.RenderMountedTrimmed(t, section, tc.width, 2)
			testkit.AssertVisual(tc.name).
				Fixture("testdata/activity_section/fixture/"+tc.fixture).
				Viewport(tc.width, 2).
				Now(t, rendered)
			if strings.Contains(rendered, "TIME") || strings.Contains(rendered, "TARGET / CLIENT") {
				t.Fatalf("rendered Activity column labels; want headerless semantic grid\n%s", rendered)
			}
			if got := physicalRowCount(rendered); got != 2 {
				t.Fatalf("rendered physical rows = %d, want section title + one event\n%s", got, rendered)
			}
		})
	}
}

func TestSection_RendersFailedLatest(t *testing.T) {
	m := successfulModel()
	m.Activity.Latest.ObservedAt = "14:35:10"
	m.Activity.Latest.Status = readmodel.ActivityFailed
	m.Activity.Latest.HTTPStatus = 500
	m.Activity.Latest.Duration = 312 * time.Millisecond
	section := Section(m, context.Background(), nil)
	rendered := testkit.RenderMountedTrimmed(t, section, 56, 2)
	testkit.AssertVisual("failed_narrow").
		Fixture("testdata/activity_section/fixture/failed_narrow.txt").
		Viewport(56, 2).
		Now(t, rendered)
}

func TestActivityDurationLabelUsesHumanReadableUnits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{name: "zero", duration: 0, want: "0s"},
		{name: "milliseconds", duration: 23 * time.Millisecond, want: "23ms"},
		{name: "sub_second_boundary", duration: 999 * time.Millisecond, want: "999ms"},
		{name: "whole_second", duration: time.Second, want: "1s"},
		{name: "fractional_seconds", duration: 5123 * time.Millisecond, want: "5.1s"},
		{name: "whole_minute", duration: time.Minute, want: "1m0s"},
		{name: "minutes_and_seconds", duration: 65 * time.Second, want: "1m5s"},
		{name: "minutes_round_to_seconds", duration: 65*time.Second + 500*time.Millisecond, want: "1m6s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := readmodel.ActivityRowReadModel{Status: readmodel.ActivitySucceeded, Duration: tc.duration, DurationKnown: true}
			if got := activityDurationLabel(row); got != tc.want {
				t.Fatalf("activityDurationLabel(%s) = %q, want %q", tc.duration, got, tc.want)
			}
		})
	}
}

func TestActivityDurationLabelHidesUnknownAndPendingTiming(t *testing.T) {
	if got := activityDurationLabel(readmodel.ActivityRowReadModel{}); got != "" {
		t.Fatalf("unknown duration = %q, want empty", got)
	}
	if got := activityDurationLabel(readmodel.ActivityRowReadModel{
		Status: readmodel.ActivityPending, Duration: time.Second, DurationKnown: true,
	}); got != "" {
		t.Fatalf("pending duration = %q, want empty", got)
	}
}

func TestActivityStatusLabelUsesRequestStateOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  readmodel.ActivityRowReadModel
		want string
	}{
		{name: "in_progress_ignores_nonterminal_http_field", row: readmodel.ActivityRowReadModel{Status: readmodel.ActivityPending, HTTPStatus: 200}, want: "…"},
		{name: "http", row: readmodel.ActivityRowReadModel{Status: readmodel.ActivityFailed, HTTPStatus: 429}, want: "429"},
		{name: "canceled_without_http_does_not_invent_copy", row: readmodel.ActivityRowReadModel{Status: readmodel.ActivityCanceled}, want: ""},
		{name: "failed_without_http_does_not_invent_copy", row: readmodel.ActivityRowReadModel{Status: readmodel.ActivityFailed}, want: ""},
		{name: "completed_without_http_does_not_invent_copy", row: readmodel.ActivityRowReadModel{Status: readmodel.ActivitySucceeded}, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := activityStatusLabel(tc.row); got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSection_RendersFailoverAttemptEvidence(t *testing.T) {
	m := successfulModel()
	m.Activity.Latest.AttemptCount = 2
	section := Section(m, context.Background(), nil)
	rendered := testkit.RenderMountedTrimmed(t, section, 100, 2)
	testkit.AssertVisual("failover_narrow").
		Fixture("testdata/activity_section/fixture/failover_narrow.txt").
		Viewport(100, 2).
		Now(t, rendered)

	m.Activity.Latest.Status = readmodel.ActivityCanceled
	m.Activity.Latest.HTTPStatus = 0
	m.Activity.Latest.Duration = 23 * time.Millisecond
	rendered = testkit.RenderMountedTrimmed(t, Section(m, context.Background(), nil), 100, 2)
	testkit.AssertVisual("canceled_failover_narrow").
		Fixture("testdata/activity_section/fixture/canceled_failover_narrow.txt").
		Viewport(100, 2).
		Now(t, rendered)
}

func TestSection_RendersEmptyState(t *testing.T) {
	m := activityModel(readmodel.ActivityReadModel{})
	section := Section(m, context.Background(), nil)
	rendered := testkit.RenderMountedTrimmed(t, section, 80, 2)
	testkit.AssertVisual("empty").
		Fixture("testdata/activity_section/fixture/empty.txt").
		Viewport(80, 2).
		Now(t, rendered)
}

func TestSection_QueriesLatestActivity(t *testing.T) {
	stale := activityModel(readmodel.ActivityReadModel{})
	fresh := successfulModel()
	query := &fakeActivityQueries{activity: fresh.Activity}

	section := Section(stale, context.Background(), query)
	h, err := testkit.NewHarnessAt(section, 80, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	rendered := h.FrameTrimmed()
	testkit.AssertNow(t, rendered, testkit.All(
		testkit.Text("14:32:01").Exists(),
		testkit.Text("openai/gpt-4.1").Exists(),
	))
}

func TestSection_ShowsNoRowWhenQueryErrors(t *testing.T) {
	stale := activityModel(readmodel.ActivityReadModel{})
	query := &fakeActivityQueries{err: context.DeadlineExceeded}

	section := Section(stale, context.Background(), query)
	rendered := testkit.RenderMountedTrimmed(t, section, 80, 3)
	testkit.AssertNow(t, rendered, testkit.Text("no requests yet").Exists())
}

func TestSection_QueriesRecentActivity(t *testing.T) {
	stale := activityModel(readmodel.ActivityReadModel{})
	query := &fakeActivityQueries{activity: successfulModel().Activity}

	section := Section(stale, context.Background(), query)
	h, err := testkit.NewHarnessAt(section, 80, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()

	if got, want := query.limit(), 5; got != want {
		t.Fatalf("query limit = %d, want %d", got, want)
	}
}

func TestSection_ActivityProjectionIsPureDuringRender(t *testing.T) {
	query := &fakeActivityQueries{activity: successfulModel().Activity}
	section := Section(activityModel(readmodel.ActivityReadModel{}), context.Background(), query)

	_ = section.activityRows()

	if got := query.calls(); got != 0 {
		t.Fatalf("activityRows query calls = %d, want render projection without I/O", got)
	}
}

func TestSection_RefreshesActivityWithoutOperatorInput(t *testing.T) {
	pending := *successfulModel().Activity.Latest
	pending.Status = readmodel.ActivityPending
	pending.HTTPStatus = 0
	pending.Duration = 0
	pending.DurationKnown = false
	stale := activityModel(readmodel.ActivityReadModel{Rows: []readmodel.ActivityRowReadModel{pending}})
	query := &fakeActivityQueries{
		activity:           stale.Activity,
		activityAfterFirst: successfulModel().Activity,
	}
	section := Section(stale, context.Background(), query)

	h, err := testkit.NewHarnessAt(section, 80, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	testkit.AssertNow(t, h.FrameTrimmed(), testkit.All(
		testkit.Text("…").Exists(),
		testkit.Not(testkit.Text("pending").Exists()),
		testkit.Not(testkit.Text("145ms").Exists()),
	))
	query.armFreshActivity()

	select {
	case <-query.freshCall:
	case <-time.After(2 * time.Second):
		t.Fatalf("activity query calls after settled mount = 0, want refresh without input (total calls %d)", query.calls())
	}
	frame := h.FrameTrimmed()
	testkit.AssertNow(t, frame, testkit.All(
		testkit.Text("14:32:01").Exists(),
		testkit.Text("openai/gpt-4.1").Exists(),
		testkit.Not(testkit.Text("…").Exists()),
		testkit.Text("200").Exists(),
		testkit.Text("145ms").Exists(),
	))
}

func TestActivityInProgressGlyphStandsAloneInStatusCell(t *testing.T) {
	row := *successfulModel().Activity.Latest
	row.Status = readmodel.ActivityPending
	row.HTTPStatus = 0
	row.DurationKnown = false
	rendered := testkit.RenderMountedTrimmed(t, Section(
		activityModel(readmodel.ActivityReadModel{Rows: []readmodel.ActivityRowReadModel{row}}),
		context.Background(), nil,
	), 100, 2)
	if strings.Contains(rendered, "pending") {
		t.Fatalf("rendered literal pending copy; want standalone in-progress glyph\n%s", rendered)
	}
	testkit.AssertNow(t, rendered, testkit.All(
		testkit.Text("…").Exists(),
		testkit.Not(testkit.Text("0ms").Exists()),
	))
}

func TestSection_RendersMultipleRecentRows(t *testing.T) {
	first := *successfulModel().Activity.Latest
	first.RouteLabel = "chatgpt-dmytrii"
	first.ProviderSpec = "chatgpt"
	first.ProviderModel = "gpt-5.6-sol"
	first.ClientLabel = "codex-tui/0.146.1"
	first.Status = readmodel.ActivityPending
	first.HTTPStatus = 0
	first.DurationKnown = false
	first.AttemptCount = 1
	second := first
	second.ID = "req-2"
	second.RouteLabel = "chatgpt-gmetrofun"
	second.Status = readmodel.ActivitySucceeded
	second.HTTPStatus = 200
	second.Duration = 5713 * time.Millisecond
	second.DurationKnown = true
	second.AttemptCount = 2
	third := second
	third.ID = "req-3"
	third.RouteLabel = "chatgpt-jesus"
	third.Status = readmodel.ActivityFailed
	third.HTTPStatus = 500
	third.Duration = 63 * time.Second
	third.AttemptCount = 1
	m := activityModel(readmodel.ActivityReadModel{Rows: []readmodel.ActivityRowReadModel{first, second, third}})
	rendered := testkit.RenderMountedTrimmed(t, Section(m, context.Background(), nil), 100, 4)
	testkit.AssertVisual("multiple_rows").
		Fixture("testdata/activity_section/fixture/multiple_rows.txt").
		Viewport(100, 4).
		Now(t, rendered)
	if got := physicalRowCount(rendered); got != 4 {
		t.Fatalf("rendered physical rows = %d, want section title + three events\n%s", got, rendered)
	}
}

func physicalRowCount(rendered string) int {
	return len(strings.Split(strings.TrimSuffix(rendered, "\n"), "\n"))
}

type fakeActivityQueries struct {
	mu                 sync.Mutex
	activity           readmodel.ActivityReadModel
	activityAfterFirst readmodel.ActivityReadModel
	err                error
	lastLimit          int
	callCount          int
	freshArmed         bool
	freshCall          chan struct{}
}

func (f *fakeActivityQueries) ListActivity(_ context.Context, req ports.ListActivityRequest) (readmodel.ActivityReadModel, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastLimit = req.Limit
	f.callCount++
	if f.err != nil {
		return readmodel.ActivityReadModel{}, f.err
	}
	if f.freshArmed && !f.activityAfterFirst.IsEmpty() {
		select {
		case f.freshCall <- struct{}{}:
		default:
		}
		return f.activityAfterFirst, nil
	}
	return f.activity, nil
}

func (f *fakeActivityQueries) armFreshActivity() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.freshArmed = true
	f.freshCall = make(chan struct{}, 1)
}

func (f *fakeActivityQueries) limit() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastLimit
}

func (f *fakeActivityQueries) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

func successfulModel() readmodel.WorkspaceReadModel {
	row := readmodel.ActivityRowReadModel{
		ID:            "req-1",
		ObservedAt:    "14:32:01",
		ClientLabel:   "codex",
		RouteID:       "gpt",
		ProviderSpec:  "openai",
		ProviderModel: "gpt-4.1",
		Status:        readmodel.ActivitySucceeded,
		HTTPStatus:    200,
		Duration:      145 * time.Millisecond,
		DurationKnown: true,
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
