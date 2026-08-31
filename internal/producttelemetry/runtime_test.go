package producttelemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
)

func newRuntimeTestStore(t *testing.T) store {
	t.Helper()
	return store{
		dir: filepath.Join(t.TempDir(), "telemetry"),
		now: func() time.Time { return time.Unix(1000, 0).UTC() },
	}
}

func runtimeTestState(t *testing.T, endpoint string) *runtimeState {
	t.Helper()
	st := newRuntimeTestStore(t)
	identity, err := st.loadOrCreateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return &runtimeState{
		store: st, uploader: newReportUploader(endpoint), version: "0.1.0",
		now: func() time.Time { return time.Unix(1000, 0).UTC() }, identity: identity,
		preference: preference{Enabled: true}, reducer: newReportReducer(),
		uploadTimeout: 100 * time.Millisecond,
	}
}

func observeRuntimeEvent(t *testing.T, reducer *reportReducer, requestID string) {
	t.Helper()
	reducer.Observe(terminalTrafficEvent(t, requestID, "Codex/1.2", "openai", "/responses",
		trafficevidence.ResultClassSuccess, 200, 40, "succeeded", "", 1, false))
}

func TestRuntimeFlushRetainsPendingAndPreservesNewActiveEvents(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, append([]byte(nil), body...))
		attempts++
		current := attempts
		mu.Unlock()
		if current == 1 {
			http.Error(w, "no", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	s := runtimeTestState(t, server.URL)
	observeRuntimeEvent(t, s.reducer, "req_1")
	s.flush()
	if s.pending == nil || !s.reducer.Empty() {
		t.Fatal("failed upload must retain pending and reset active reducer")
	}
	s.reducer.Observe(terminalTrafficEvent(t, "req_2", "Claude-Code/1.0", "anthropic", "/messages",
		trafficevidence.ResultClassSuccess, 200, 40, "succeeded", "", 1, false))
	s.flush()
	if s.pending != nil || s.reducer.Empty() {
		t.Fatal("successful retry must clear only pending and preserve active events")
	}
	s.flush()
	if !s.reducer.Empty() {
		t.Fatal("third flush must send the active report")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 || string(bodies[0]) != string(bodies[1]) || string(bodies[1]) == string(bodies[2]) {
		t.Fatalf("unexpected retry bodies: count=%d", len(bodies))
	}
}

func TestRuntimeInternalPanicIsContained(t *testing.T) {
	state := runtimeTestState(t, "http://127.0.0.1:1")
	commands := make(chan runtimeCommand, 1)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go state.run(ctx, commands, done)
	reply := make(chan inspectResponse)
	close(reply)
	commands <- runtimeCommand{inspect: reply}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("telemetry actor panic was not contained")
	}
}

func TestRuntimeDebugConstructsWithoutTransmission(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	s := runtimeTestState(t, server.URL)
	s.debug = true
	observeRuntimeEvent(t, s.reducer, "req_1")
	s.flush()
	if calls.Load() != 0 {
		t.Fatalf("debug transmitted %d requests", calls.Load())
	}
}

func TestRuntimeCloseDrainsAcceptedEventsAndBoundsFlush(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()
	r := startRuntime(runtimeConfig{
		store: newRuntimeTestStore(t), Version: "0.1.0", Endpoint: server.URL,
		FirstFlushDelay: time.Hour, Cadence: time.Hour, UploadTimeout: 20 * time.Millisecond,
	})
	r.Observe(terminalTrafficEvent(t, "req_1", "Codex/1.2", "openai", "/responses",
		trafficevidence.ResultClassSuccess, 200, 40, "succeeded", "", 1, false))
	started := time.Now()
	r.Close()
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("Close took %s", elapsed)
	}
	select {
	case <-requestStarted:
	default:
		t.Fatal("shutdown did not attempt a flush")
	}
}

func TestRuntimeInspectIsObservationalAndDoesNotTransmit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	r := startRuntime(runtimeConfig{
		store: newRuntimeTestStore(t), Version: "0.1.0", Endpoint: server.URL,
		FirstFlushDelay: time.Hour, Cadence: time.Hour,
	})
	defer r.Close()
	r.Observe(terminalTrafficEvent(t, "req_1", "Codex/1.2", "openai", "/responses",
		trafficevidence.ResultClassSuccess, 200, 40, "succeeded", "", 1, false))
	body, err := r.InspectJSON(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var report productReport
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Traffic) != 1 || calls.Load() != 0 {
		t.Fatalf("traffic=%d calls=%d", len(report.Traffic), calls.Load())
	}
	if report.ReportID == "" || report.ReportCreatedAt == "" || r == nil {
		t.Fatalf("inspect did not return a valid V2 preview: %+v", report)
	}
}

func TestRuntimeStateInspectDoesNotFreezeOrResetActiveState(t *testing.T) {
	s := runtimeTestState(t, "http://127.0.0.1:1")
	observeRuntimeEvent(t, s.reducer, "req_1")
	before := s.reducer.snapshot(s.identity.InstallID, s.version, "linux", "amd64")
	result := s.inspect()
	if result.err != nil || string(result.body) == "null" {
		t.Fatalf("inspect result = %q err=%v", result.body, result.err)
	}
	after := s.reducer.snapshot(s.identity.InstallID, s.version, "linux", "amd64")
	if !reflect.DeepEqual(before.Traffic, after.Traffic) || before.OverflowCount != after.OverflowCount {
		t.Fatalf("inspect mutated reducer: before=%+v after=%+v", before, after)
	}
	if s.pending != nil {
		t.Fatalf("inspect created pending report: %+v", *s.pending)
	}
}

// TestRuntime_refreshPreference_ResetsOnRevisionChange: when the persisted
// preference revision changes, the aggregate collected under the prior revision
// is discarded. runtimeState is constructed directly (no goroutine) so its fields
// are test-owned.
func TestRuntime_refreshPreference_ResetsOnRevisionChange(t *testing.T) {
	st := newRuntimeTestStore(t)
	s := &runtimeState{store: st, reducer: newReportReducer(), now: func() time.Time { return time.Unix(1000, 0).UTC() }}

	if enabled := s.refreshPreference(); !enabled || s.preference.Revision != "" {
		t.Fatalf("initial adopt: enabled=%v revision=%q", enabled, s.preference.Revision)
	}
	s.reducer.Observe(terminalTrafficEvent(t, "req_1", "Codex/1.2", "openai", "/responses",
		trafficevidence.ResultClassSuccess, 200, 40, "succeeded", "", 1, false))
	if s.reducer.Empty() {
		t.Fatal("expected the reducer to hold the observed event")
	}

	if err := st.setEnabled(false); err != nil { // writes a fresh revision
		t.Fatalf("setEnabled(false): %v", err)
	}
	if s.refreshPreference() {
		t.Fatal("refreshPreference = true after opt-out, want false")
	}
	if !s.reducer.Empty() {
		t.Fatal("aggregate collected under the prior revision was not discarded")
	}
}

// TestStartRuntime_NilOnDONotTrack: DO_NOT_TRACK means no runtime is constructed.
func TestStartRuntime_NilOnDoNotTrack(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "1")
	if r := startRuntime(runtimeConfig{store: newRuntimeTestStore(t), Version: "0.1.0"}); r != nil {
		r.Close()
		t.Fatal("StartRuntime returned a runtime under DO_NOT_TRACK, want nil")
	}
}
