package bootstrap

import (
	"context"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/telemetry"
)

const (
	embeddedTelemetryEndpoint       = "https://swobu.com"
	embeddedTelemetryExportInterval = time.Hour
	errorTraceRateWindow            = time.Minute
)

type embeddedTelemetryRuntimeState struct {
	store                 telemetry.Store
	emitter               telemetry.Emitter
	now                   func() time.Time
	once                  sync.Once
	stopCh                chan struct{}
	doneCh                chan struct{}
	eventsCh              chan runtimeevidence.TrafficEvent
	seenTerminalRequestID map[string]struct{}
	errorTraceWindowStart time.Time
	errorTracesInWindow   int
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
		d.telemetry.eventsCh = make(chan runtimeevidence.TrafficEvent, 512)
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
	if !d.ensureTelemetryNoticeState() {
		return
	}
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

func (d *Daemon) ensureTelemetryNoticeState() bool {
	state, err := d.telemetry.store.LoadOrCreate()
	if err != nil {
		return false
	}
	return state.NoticeShown
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

func (d *Daemon) emitEventTelemetryBestEffort(ctx context.Context, event runtimeevidence.TrafficEvent) {
	if d == nil || d.telemetry.emitter == nil {
		return
	}
	if event.EventKind() != runtimeevidence.EventKindUpstreamTerminal {
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
	if d.endpoints != nil {
		if status, err := d.Status(); err == nil {
			state = string(status.State)
		}
	}
	d.telemetry.emitter.EmitCounts(ctx, state, d2xx, d429, d4xx, d5xx)
	d.emitErrorTraceForEventBestEffort(ctx, event)
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

func (d *Daemon) observeTelemetryEvent(event runtimeevidence.TrafficEvent) {
	if d == nil || d.telemetry.eventsCh == nil {
		return
	}
	select {
	case d.telemetry.eventsCh <- event:
	default:
		// Telemetry must not block request-path evidence writes.
	}
}

func (d *Daemon) emitErrorTraceForEventBestEffort(ctx context.Context, event runtimeevidence.TrafficEvent) {
	if d == nil || d.telemetry.emitter == nil {
		return
	}
	limit := telemetryErrorTraceMaxPerTick()
	if limit <= 0 {
		return
	}
	if event.StatusCode() < 400 {
		return
	}
	now := d.telemetry.now
	if now == nil {
		now = time.Now
	}
	current := now()
	if d.telemetry.errorTraceWindowStart.IsZero() || current.Sub(d.telemetry.errorTraceWindowStart) >= errorTraceRateWindow {
		d.telemetry.errorTraceWindowStart = current
		d.telemetry.errorTracesInWindow = 0
	}
	if d.telemetry.errorTracesInWindow >= limit {
		return
	}
	d.telemetry.errorTracesInWindow++
	trace := telemetry.ErrorTrace{
		StatusCode:    event.StatusCode(),
		ResultClass:   strings.TrimSpace(event.Result().String()),      // trimlowerlint:allow boundary canonicalization
		ProviderRoute: strings.TrimSpace(event.Route().String()),       // trimlowerlint:allow boundary canonicalization
		Operation:     strings.TrimSpace(string(event.NormalizedOp())), // trimlowerlint:allow boundary canonicalization
	}
	if durationMS, ok := event.Timing().DurationMillis(); ok {
		trace.DurationMS = &durationMS
	}
	if telemetryTraceDebugEnabled() {
		trace.DebugRawStack = string(debug.Stack())
	}
	d.telemetry.emitter.EmitErrorTrace(ctx, trace)
}

func telemetryTraceDebugEnabled() bool {
	return platformconfig.EnvTruthy(os.Getenv(platformconfig.EnvTelemetryDebugTraceStack))
}

func telemetryErrorTraceMaxPerTick() int {
	raw := strings.TrimSpace(os.Getenv(platformconfig.EnvTelemetryErrorTraceMaxPerTick)) // trimlowerlint:allow boundary canonicalization
	if raw == "" {
		return 20
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 20
	}
	return n
}
