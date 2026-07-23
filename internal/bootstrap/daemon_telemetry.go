package bootstrap

import (
	"context"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/telemetry"
)

const (
	embeddedTelemetryEndpoint       = "https://swobu.com"
	embeddedTelemetryExportInterval = time.Hour
)

type embeddedTelemetryRuntimeState struct {
	store                 telemetry.Store
	emitter               telemetry.Emitter
	now                   func() time.Time
	once                  sync.Once
	stopCh                chan struct{}
	doneCh                chan struct{}
	eventsCh              chan trafficevidence.TrafficEvent
	seenTerminalRequestID map[string]struct{}
}

func (d *Daemon) startTelemetryRuntime() {
	if d == nil {
		return
	}
	if d.telemetry.now == nil {
		d.telemetry.now = time.Now
	}
	if d.telemetry.seenTerminalRequestID == nil {
		d.telemetry.seenTerminalRequestID = make(map[string]struct{})
	}
	if d.telemetry.stopCh != nil {
		return
	}
	d.telemetry.stopCh = make(chan struct{})
	d.telemetry.doneCh = make(chan struct{})
	if d.telemetry.eventsCh == nil {
		d.telemetry.eventsCh = make(chan trafficevidence.TrafficEvent, 512)
	}
	go d.runTelemetryRuntime()
}

func (d *Daemon) stopTelemetryRuntimeWithContext(ctx context.Context) {
	if d == nil || d.telemetry.stopCh == nil {
		return
	}
	d.telemetry.once.Do(func() { close(d.telemetry.stopCh) })
	select {
	case <-d.telemetry.doneCh:
	case <-ctx.Done():
	}
}

func (d *Daemon) runTelemetryRuntime() {
	defer close(d.telemetry.doneCh)
	if !d.initTelemetryEmitter(context.Background()) {
		return
	}
	defer func() {
		if d.telemetry.emitter != nil {
			_ = d.telemetry.emitter.Shutdown(context.Background())
		}
	}()

	d.emitInstallTelemetryBestEffort(context.Background())
	for {
		select {
		case <-d.telemetry.stopCh:
			return
		case event := <-d.telemetry.eventsCh:
			d.emitEventTelemetryBestEffort(context.Background(), event)
		}
	}
}

func telemetryDebugEnabled() bool {
	return platformconfig.EnvTruthy(os.Getenv(platformconfig.EnvTelemetryDebugStdoutSink))
}

func (d *Daemon) initTelemetryEmitter(ctx context.Context) bool {
	if telemetryDebugEnabled() {
		d.telemetry.emitter = telemetry.NewStdoutEmitter(os.Stdout)
		return true
	}
	emitter, err := telemetry.NewMetricsEmitter(ctx, telemetry.MetricsEmitterConfig{
		EndpointURL:    platformconfig.ResolveTelemetryEndpoint(embeddedTelemetryEndpoint),
		Timeout:        5 * time.Second,
		ExportInterval: telemetryExportInterval(),
	})
	if err != nil {
		if d.logger != nil {
			d.logger.Warn("telemetry init failed", "error", err.Error())
		}
		return false
	}
	d.telemetry.emitter = emitter
	return true
}

func (d *Daemon) emitInstallTelemetryBestEffort(ctx context.Context) {
	if d.telemetry.emitter == nil {
		return
	}
	state, err := d.telemetry.store.LoadOrCreate()
	if err != nil {
		return
	}
	d.telemetry.emitter.EmitInstall(ctx, state, controlplane.SwobuVersion(), runtime.GOOS, runtime.GOARCH)
}

func (d *Daemon) emitEventTelemetryBestEffort(ctx context.Context, event trafficevidence.TrafficEvent) {
	if d == nil || d.telemetry.emitter == nil {
		return
	}
	if event.EventKind() != trafficevidence.EventKindProviderTerminal {
		return
	}
	requestID := event.RequestID().String()
	if requestID == "" {
		return
	}
	if d.telemetry.seenTerminalRequestID == nil {
		d.telemetry.seenTerminalRequestID = make(map[string]struct{})
	}
	if _, seen := d.telemetry.seenTerminalRequestID[requestID]; seen {
		return
	}
	d.telemetry.seenTerminalRequestID[requestID] = struct{}{}
	d2xx, d429, d4xx, d5xx := classifyStatusCodeCounters(event.StatusCode())
	if d2xx == 0 && d429 == 0 && d4xx == 0 && d5xx == 0 {
		return
	}
	state := string(HealthStateHealthy)
	if d.configStore != nil {
		if status, err := d.Status(); err == nil {
			state = string(status.State)
		}
	}
	d.telemetry.emitter.EmitCounts(ctx, state, d2xx, d429, d4xx, d5xx)
	d.emitErrorCounterBestEffort(ctx, event)
}

func classifyStatusCodeCounters(statusCode int) (int64, int64, int64, int64) {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return 1, 0, 0, 0
	case statusCode == 429:
		return 0, 1, 0, 0
	case statusCode >= 400 && statusCode < 500:
		return 0, 0, 1, 0
	case statusCode >= 500:
		return 0, 0, 0, 1
	default:
		return 0, 0, 0, 0
	}
}

func telemetryExportInterval() time.Duration {
	return platformconfig.ResolveTelemetryExportInterval(embeddedTelemetryExportInterval)
}

func (d *Daemon) observeTelemetryEvent(event trafficevidence.TrafficEvent) {
	if d == nil || d.telemetry.eventsCh == nil {
		return
	}
	select {
	case d.telemetry.eventsCh <- event:
	default:
		// Telemetry must not block request-path evidence writes.
	}
}

// emitErrorCounterBestEffort records a bounded, content-free error signal as
// aggregate counter attributes (result class × provider family × operation ×
// duration bucket). No message, stack, route, or identifier is emitted — this
// is the anonymous replacement for the deleted OTLP error-span path.
func (d *Daemon) emitErrorCounterBestEffort(ctx context.Context, event trafficevidence.TrafficEvent) {
	if d == nil || d.telemetry.emitter == nil {
		return
	}
	if event.StatusCode() < 400 {
		return
	}
	ms, hasDuration := event.Timing().DurationMillis()
	signal := telemetry.ErrorSignal{
		ResultClass:    strings.TrimSpace(event.Result().String()),      // swobu:io-string source=boundary
		ProviderFamily: telemetry.NormalizeProviderFamily(event.Route().String()),
		Operation:      strings.TrimSpace(string(event.NormalizedOp())), // swobu:io-string source=boundary
		DurationBucket: bucketDurationMillis(ms, hasDuration),
	}
	d.telemetry.emitter.EmitError(ctx, signal)
}

// bucketDurationMillis collapses a request duration into a bounded bucket so
// telemetry never carries per-request timing (re-identifying when correlated
// with an observed address).
func bucketDurationMillis(ms int, ok bool) string {
	if !ok {
		return "unknown"
	}
	switch {
	case ms < 100:
		return "0_100ms"
	case ms < 500:
		return "100_500ms"
	case ms < 1000:
		return "500ms_1s"
	case ms < 5000:
		return "1_5s"
	default:
		return "5s_plus"
	}
}
