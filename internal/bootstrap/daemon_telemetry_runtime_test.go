package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
	"github.com/swobuforge/swobu/internal/telemetry"
)

type fakeTelemetryEmitter struct {
	installCalls int
	countCalls   int
	errorTraces  []telemetry.ErrorTrace
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

func (e *fakeTelemetryEmitter) EmitErrorTrace(_ context.Context, trace telemetry.ErrorTrace) {
	e.errorTraces = append(e.errorTraces, trace)
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

	event := mustTerminalTrafficEvent(t, "req_1", runtimeevidence.ResultClassSuccess, 200)
	daemon.emitEventTelemetryBestEffort(context.Background(), event)
	daemon.emitEventTelemetryBestEffort(context.Background(), event)

	if emitter.countCalls != 1 {
		t.Fatalf("count calls=%d, want 1 after duplicate terminal event", emitter.countCalls)
	}
	if emitter.last2xx != 1 || emitter.last429 != 0 || emitter.last4xx != 0 || emitter.last5xx != 0 {
		t.Fatalf("counts 2xx=%d 429=%d 4xx=%d 5xx=%d", emitter.last2xx, emitter.last429, emitter.last4xx, emitter.last5xx)
	}
}

func TestEmitEventTelemetryBestEffort_EmitsCappedErrorTracesWithoutRawStackByDefault(t *testing.T) {
	t.Setenv("SWOBU_TELEMETRY_ERROR_TRACE_MAX_PER_TICK", "1")
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
	daemon.emitEventTelemetryBestEffort(context.Background(), mustTerminalTrafficEvent(t, "req_a", runtimeevidence.ResultClassBackendError, 500))
	daemon.emitEventTelemetryBestEffort(context.Background(), mustTerminalTrafficEvent(t, "req_b", runtimeevidence.ResultClassBackendError, 500))

	if got := len(emitter.errorTraces); got != 1 {
		t.Fatalf("error traces=%d, want 1 (capped)", got)
	}
	if emitter.errorTraces[0].DebugRawStack != "" {
		t.Fatal("debug raw stack present by default, want empty")
	}
}

func TestEmitEventTelemetryBestEffort_EmitsRawStackOnlyInTraceDebugMode(t *testing.T) {
	t.Setenv("SWOBU_TELEMETRY_ERROR_TRACE_MAX_PER_TICK", "2")
	t.Setenv("SWOBU_TELEMETRY_ERROR_TRACE_STACK_DEBUG", "1")
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
	daemon.emitEventTelemetryBestEffort(context.Background(), mustTerminalTrafficEvent(t, "req_1", runtimeevidence.ResultClassBackendError, 500))
	if len(emitter.errorTraces) != 1 {
		t.Fatalf("error traces=%d, want 1", len(emitter.errorTraces))
	}
	if emitter.errorTraces[0].DebugRawStack == "" {
		t.Fatal("debug raw stack empty, want populated in trace debug mode")
	}
}

func mustTerminalTrafficEvent(t *testing.T, requestID string, result runtimeevidence.ResultClass, statusCode int) runtimeevidence.TrafficEvent {
	t.Helper()
	id, err := runtimeevidence.ParseRequestID(requestID)
	if err != nil {
		t.Fatalf("ParseRequestID returned error: %v", err)
	}
	route, err := runtimeevidence.NewRoute("openai", "gpt-4.1")
	if err != nil {
		t.Fatalf("NewRoute returned error: %v", err)
	}
	event, err := runtimeevidence.NewTerminalTrafficEvent(runtimeevidence.TrafficEventInput{
		RequestID:      id,
		Endpoint:       "default",
		ClientProtocol: runtimeevidence.ClientProtocol("responses"),
		ClientHandler:  runtimeevidence.ClientHandler("http"),
		IngressFamily:  runtimeevidence.IngressFamily("openai"),
		NormalizedOp:   runtimeevidence.NormalizedOp("responses.create"),
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
  "anonymous_install_id": "anon_test",
  "first_seen_at": "2026-04-30T00:00:00Z",
  "notice_shown": true
}`
	if err := os.WriteFile(statePath, []byte(state), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return statePath
}
