package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	evidencestore "github.com/metrofun/swobu/internal/adapters/outbound/evidence"
	"github.com/metrofun/swobu/internal/telemetry"
)

type fakeTelemetryProjectionSource struct {
	calls int
	scope evidencestore.ProjectionScope
	out   evidencestore.StatusProjection
	err   error
}

func (s *fakeTelemetryProjectionSource) StatusProjectionForScope(scope evidencestore.ProjectionScope) (evidencestore.StatusProjection, error) {
	s.calls++
	s.scope = scope
	if s.err != nil {
		return evidencestore.StatusProjection{}, s.err
	}
	return s.out, nil
}

type fakeTelemetryEmitter struct {
	installCalls int
	countCalls   int
	lastState    string
	last2xx      int64
	last429      int64
	last4xx      int64
	last5xx      int64
}

func (e *fakeTelemetryEmitter) Shutdown(context.Context) error { return nil }

func (e *fakeTelemetryEmitter) EmitInstall(context.Context, telemetry.State, string, string, string) {
	e.installCalls++
}

func (e *fakeTelemetryEmitter) EmitCounts(_ context.Context, state string, count2xx, count429, count4xx, count5xx int64) {
	e.countCalls++
	e.lastState = state
	e.last2xx = count2xx
	e.last429 = count429
	e.last4xx = count4xx
	e.last5xx = count5xx
}

func TestEmitProjectionTelemetryBestEffort_UsesProjectionSource(t *testing.T) {
	statePath := writeTelemetryStateFixture(t)
	source := &fakeTelemetryProjectionSource{
		out: evidencestore.StatusProjection{
			State: "healthy",
			Counters: evidencestore.StatusCounters{
				Count2xx: 4,
				Count429: 1,
				Count4xx: 2,
				Count5xx: 3,
			},
		},
	}
	emitter := &fakeTelemetryEmitter{}
	daemon := &Daemon{
		telemetry: embeddedTelemetryRuntime{
			store:            telemetry.Store{StatePath: statePath},
			emitter:          emitter,
			projectionSource: source,
		},
	}

	daemon.emitProjectionTelemetryBestEffort(context.Background(), true)

	if source.calls != 1 {
		t.Fatalf("projection source calls=%d, want 1", source.calls)
	}
	if source.scope.Kind != evidencestore.ProjectionScopeAll {
		t.Fatalf("projection scope kind=%q, want %q", source.scope.Kind, evidencestore.ProjectionScopeAll)
	}
	if emitter.installCalls != 1 {
		t.Fatalf("install calls=%d, want 1", emitter.installCalls)
	}
	if emitter.countCalls != 1 {
		t.Fatalf("count calls=%d, want 1", emitter.countCalls)
	}
	if emitter.lastState != "healthy" || emitter.last2xx != 4 || emitter.last429 != 1 || emitter.last4xx != 2 || emitter.last5xx != 3 {
		t.Fatalf("counts state=%q 2xx=%d 429=%d 4xx=%d 5xx=%d", emitter.lastState, emitter.last2xx, emitter.last429, emitter.last4xx, emitter.last5xx)
	}
}

func TestEmitProjectionTelemetryBestEffort_ProjectionFailureIsIsolated(t *testing.T) {
	statePath := writeTelemetryStateFixture(t)
	source := &fakeTelemetryProjectionSource{err: fmt.Errorf("projection down")}
	emitter := &fakeTelemetryEmitter{}
	daemon := &Daemon{
		telemetry: embeddedTelemetryRuntime{
			store:            telemetry.Store{StatePath: statePath},
			emitter:          emitter,
			projectionSource: source,
		},
	}

	daemon.emitProjectionTelemetryBestEffort(context.Background(), false)

	if source.calls != 1 {
		t.Fatalf("projection source calls=%d, want 1", source.calls)
	}
	if emitter.countCalls != 0 {
		t.Fatalf("count calls=%d, want 0 on projection error", emitter.countCalls)
	}
}

func writeTelemetryStateFixture(t *testing.T) string {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	state := `{
  "enabled": true,
  "anonymous_install_id": "anon_test",
  "first_seen_at": "2026-04-30T00:00:00Z",
  "notice_shown": true
}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return statePath
}
