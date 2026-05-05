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

	evidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/evidence"
	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/telemetry"
)

const (
	embeddedTelemetryEndpoint = "https://api.swobu.com/v1/metrics"
	embeddedTelemetryInterval = 6 * time.Hour
)

type embeddedTelemetryRuntimeState struct {
	store          telemetry.Store
	emitter        telemetry.Emitter
	now            func() time.Time
	projectionLoad func(scope evidencestore.ProjectionScope) (evidencestore.StatusProjection, error)
	once           sync.Once
	stopCh         chan struct{}
	doneCh         chan struct{}
	hasLast        bool
	lastCount      evidencestore.StatusCounters
	seenRequestIDs map[string]struct{}
}

func (d *Daemon) startTelemetryRuntime() {
	if d == nil {
		return
	}
	if d.telemetry.now == nil {
		d.telemetry.now = time.Now
	}
	if d.telemetry.seenRequestIDs == nil {
		d.telemetry.seenRequestIDs = make(map[string]struct{})
	}
	if d.telemetry.stopCh != nil {
		return
	}
	d.telemetry.stopCh = make(chan struct{})
	d.telemetry.doneCh = make(chan struct{})
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

	d.emitProjectionTelemetryBestEffort(context.Background(), true)
	interval := telemetryInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-d.telemetry.stopCh:
			d.emitProjectionTelemetryBestEffort(context.Background(), false)
			return
		case <-ticker.C:
			d.emitProjectionTelemetryBestEffort(context.Background(), false)
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
		EndpointURL: embeddedTelemetryEndpoint,
		Timeout:     5 * time.Second,
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

func (d *Daemon) emitProjectionTelemetryBestEffort(ctx context.Context, includeInstall bool) {
	if d.telemetry.emitter == nil {
		return
	}
	state, err := d.telemetry.store.LoadOrCreate()
	if err != nil {
		return
	}
	if includeInstall {
		d.telemetry.emitter.EmitInstall(ctx, state, controlplane.SwobuVersion(), runtime.GOOS, runtime.GOARCH)
	}
	if d.telemetry.projectionLoad == nil {
		return
	}
	projection, err := d.telemetry.projectionLoad(evidencestore.ProjectionScope{Kind: evidencestore.ProjectionScopeAll})
	if err != nil {
		return
	}
	d2xx, d429, d4xx, d5xx := d.deltaCounters(projection.Counters)
	if d2xx == 0 && d429 == 0 && d4xx == 0 && d5xx == 0 {
		return
	}
	d.telemetry.emitter.EmitCounts(ctx, projection.State, d2xx, d429, d4xx, d5xx)
	d.emitErrorTracesBestEffort(ctx, projection)
}

func (d *Daemon) deltaCounters(c evidencestore.StatusCounters) (int64, int64, int64, int64) {
	if !d.telemetry.hasLast {
		d.telemetry.hasLast = true
		d.telemetry.lastCount = c
		return int64(c.Count2xx), int64(c.Count429), int64(c.Count4xx), int64(c.Count5xx)
	}
	prev := d.telemetry.lastCount
	d.telemetry.lastCount = c
	return nonNegativeDelta(c.Count2xx, prev.Count2xx),
		nonNegativeDelta(c.Count429, prev.Count429),
		nonNegativeDelta(c.Count4xx, prev.Count4xx),
		nonNegativeDelta(c.Count5xx, prev.Count5xx)
}

func nonNegativeDelta(current, previous int) int64 {
	delta := current - previous
	if delta < 0 {
		return 0
	}
	return int64(delta)
}

func telemetryInterval() time.Duration {
	return embeddedTelemetryInterval
}

func (d *Daemon) emitErrorTracesBestEffort(ctx context.Context, projection evidencestore.StatusProjection) {
	if d == nil || d.telemetry.emitter == nil {
		return
	}
	if d.telemetry.seenRequestIDs == nil {
		d.telemetry.seenRequestIDs = make(map[string]struct{})
	}
	limit := telemetryErrorTraceMaxPerTick()
	if limit <= 0 {
		return
	}
	emitted := 0
	debugStacks := telemetryTraceDebugEnabled()
	for _, row := range projection.RecentTraffic {
		if emitted >= limit {
			return
		}
		if row.RequestID == "" {
			continue
		}
		if _, seen := d.telemetry.seenRequestIDs[row.RequestID]; seen {
			continue
		}
		if row.StatusCode < 400 {
			continue
		}
		d.telemetry.seenRequestIDs[row.RequestID] = struct{}{}
		trace := telemetry.ErrorTrace{
			StatusCode:    row.StatusCode,
			ResultClass:   strings.TrimSpace(row.Result),
			ProviderRoute: strings.TrimSpace(row.Route),
			Operation:     strings.TrimSpace(row.NormalizedOp),
			DurationMS:    row.DurMillis,
		}
		if debugStacks {
			trace.DebugRawStack = string(debug.Stack())
		}
		d.telemetry.emitter.EmitErrorTrace(ctx, trace)
		emitted++
	}
}

func telemetryTraceDebugEnabled() bool {
	return platformconfig.EnvTruthy(os.Getenv(platformconfig.EnvTelemetryDebugTraceStack))
}

func telemetryErrorTraceMaxPerTick() int {
	raw := strings.TrimSpace(os.Getenv(platformconfig.EnvTelemetryErrorTraceMaxPerTick))
	if raw == "" {
		return 20
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 20
	}
	return n
}
