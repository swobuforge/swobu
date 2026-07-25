package producttelemetry

import (
	"path/filepath"
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
