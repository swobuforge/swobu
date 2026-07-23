package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/telemetry"
)

type fakeTelemetryEmitter struct {
	installCalls int
	countCalls   int
	errorSignals []telemetry.ErrorSignal
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

func (e *fakeTelemetryEmitter) EmitError(_ context.Context, signal telemetry.ErrorSignal) {
	e.errorSignals = append(e.errorSignals, signal)
}

func TestEmitEventTelemetryBestEffort_UsesTerminalEventAndDeduplicatesByRequestID(t *testing.T) {
	statePath := writeTelemetryStateFixture(t)
	emitter := &fakeTelemetryEmitter{}
	daemon := &Daemon{
		telemetry: embeddedTelemetryRuntimeState{
			store:                 telemetry.Store{StatePath: statePath},
			emitter:               emitter,
			now:                   time.Now,
			seenTerminalRequestID: make(map[string]struct{}),
		},
	}

	event := mustTerminalTrafficEvent(t, "req_1", trafficevidence.ResultClassSuccess, 200)
	daemon.emitEventTelemetryBestEffort(context.Background(), event)
	daemon.emitEventTelemetryBestEffort(context.Background(), event)

	if emitter.countCalls != 1 {
		t.Fatalf("count calls=%d, want 1 after duplicate terminal event", emitter.countCalls)
	}
	if emitter.last2xx != 1 || emitter.last429 != 0 || emitter.last4xx != 0 || emitter.last5xx != 0 {
		t.Fatalf("counts 2xx=%d 429=%d 4xx=%d 5xx=%d", emitter.last2xx, emitter.last429, emitter.last4xx, emitter.last5xx)
	}
}

// Every error records an anonymous, content-free counter signal — one per error,
// no cap, no stack, no route. The deleted OTLP error-span path is gone.
func TestEmitEventTelemetryBestEffort_EmitsAnonymousErrorCounterForEachError(t *testing.T) {
	statePath := writeTelemetryStateFixture(t)
	emitter := &fakeTelemetryEmitter{}
	daemon := &Daemon{
		telemetry: embeddedTelemetryRuntimeState{
			store:                 telemetry.Store{StatePath: statePath},
			emitter:               emitter,
			now:                   time.Now,
			seenTerminalRequestID: make(map[string]struct{}),
		},
	}
	daemon.emitEventTelemetryBestEffort(context.Background(), mustTerminalTrafficEvent(t, "req_a", trafficevidence.ResultClassBackendError, 500))
	daemon.emitEventTelemetryBestEffort(context.Background(), mustTerminalTrafficEvent(t, "req_b", trafficevidence.ResultClassBackendError, 500))

	if got := len(emitter.errorSignals); got != 2 {
		t.Fatalf("error signals=%d, want 2 (one per error, no cap)", got)
	}
	signal := emitter.errorSignals[0]
	if signal.ProviderFamily != "openai" {
		t.Fatalf("provider_family = %q, want openai (route collapsed to family)", signal.ProviderFamily)
	}
	if signal.Operation != "responses.create" {
		t.Fatalf("operation = %q, want responses.create", signal.Operation)
	}
}

func mustTerminalTrafficEvent(t *testing.T, requestID string, result trafficevidence.ResultClass, statusCode int) trafficevidence.TrafficEvent {
	t.Helper()
	id, err := trafficevidence.ParseRequestID(requestID)
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := trafficevidence.NewRoute("openai", "gpt-4.1")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	event, err := trafficevidence.NewTerminalTrafficEvent(trafficevidence.TrafficEventInput{RequestID: id, Workspace: "default",
		ClientProtocol: trafficevidence.ClientProtocol("responses"),
		ClientHandler:  trafficevidence.ClientHandler("http"),
		ClientFamily:   trafficevidence.ClientFamily("openai"),
		NormalizedOp:   trafficevidence.NormalizedOp("responses.create"),
		Route:          route,
		Result:         result,
		StatusCode:     statusCode,
	})
	if err != nil {
		t.Fatalf("NewTerminalTrafficEvent returned error: %v", err)
	}
	return event
}

func writeTelemetryStateFixture(t *testing.T) string {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	state := `{
  "enabled": true,
  "first_seen_at": "2026-04-30T00:00:00Z",
  "notice_shown": true
}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return statePath
}
