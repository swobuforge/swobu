package producttelemetry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	goruntime "runtime"
	"strings"
	"time"

	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

const (
	embeddedTelemetryEndpoint = "https://swobu.com"
	reportEndpointPath        = "/api/v1/telemetry"
	defaultReportCadence      = 6 * time.Hour
	firstSessionFlushDelay    = 2 * time.Minute
	// preferencePollInterval bounds how long an external preference change (e.g.
	// `swobu telemetry off`) takes to be adopted by a running daemon. The upload
	// boundary is still authoritative; this only avoids waiting a full cadence.
	preferencePollInterval = 30 * time.Second
	defaultUploadTimeout   = 2 * time.Second
)

// Runtime is a communication-only handle to the telemetry actor. All mutable
// telemetry facts live in the goroutine-local runtimeState; the handle carries
// only the channels to feed it and wait for it. Observe never blocks the request
// path; Close is idempotent (cancel is reusable and a closed done channel reads
// forever).
type Runtime struct {
	commands chan<- runtimeCommand
	cancel   context.CancelFunc
	done     <-chan struct{}
}

// runtimeConfig is the in-package wiring input. The public StartRuntime constructs
// it from a default store/endpoint/clock; tests build one directly to inject a
// temp-dir store, an endpoint, or a clock.
type runtimeConfig struct {
	store           store
	Version         string
	Endpoint        string
	Logger          *slog.Logger
	Now             func() time.Time
	Debug           bool
	Cadence         time.Duration
	FirstFlushDelay time.Duration
	UploadTimeout   time.Duration
}

// StartRuntime launches the product-telemetry runtime for the daemon lifetime
// against the default telemetry state directory, report endpoint, and wall clock.
// It returns nil when a prerequisite is unavailable: DO_NOT_TRACK is set, the
// canonical build version is not a bounded printable-ASCII token, or the
// identity/preference state cannot be loaded. Telemetry is best-effort — a nil
// runtime is a valid "collect nothing" state. Tests that inject a store, endpoint,
// or clock call startRuntime directly.
func StartRuntime(version string, logger *slog.Logger) *Runtime {
	return startRuntime(runtimeConfig{
		store:    newStore(),
		Version:  version,
		Logger:   logger,
		Endpoint: resolveReportEndpoint(),
		Now:      time.Now,
		Debug:    os.Getenv("SWOBU_TELEMETRY_DEBUG") == "1",
	})
}

func startRuntime(cfg runtimeConfig) *Runtime {
	logger := cfg.Logger
	if DoNotTrackEnabled() {
		if logger != nil {
			logger.Info("product telemetry disabled: DO_NOT_TRACK set")
		}
		return nil
	}
	if !validReportVersion(cfg.Version) {
		// Fail closed: a malformed canonical build value would otherwise collect a
		// full period only for the Worker to reject the report.
		if logger != nil {
			logger.Warn("product telemetry disabled: build version is not a bounded printable-ASCII token", "version", cfg.Version)
		}
		return nil
	}
	identity, err := cfg.store.loadOrCreateIdentity()
	if err != nil {
		logDisabled(logger, "identity", err)
		return nil
	}
	preference, err := cfg.store.preference()
	if err != nil {
		logDisabled(logger, "preference", err)
		return nil
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = resolveReportEndpoint()
	}
	s := &runtimeState{
		store:           cfg.store,
		uploader:        newReportUploader(endpoint),
		version:         cfg.Version,
		logger:          logger,
		now:             now,
		identity:        identity,
		preference:      preference,
		reducer:         newReportReducer(),
		debug:           cfg.Debug,
		cadence:         durationOr(cfg.Cadence, defaultReportCadence),
		firstFlushDelay: durationOr(cfg.FirstFlushDelay, firstSessionFlushDelay),
		uploadTimeout:   durationOr(cfg.UploadTimeout, defaultUploadTimeout),
	}
	commands := make(chan runtimeCommand, 512)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go s.run(ctx, commands, done)
	return &Runtime{commands: commands, cancel: cancel, done: done}
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

// Observe offers one terminal traffic event to the actor. It never blocks the
// request path: if the buffer is full the event is dropped (telemetry is lossy).
func (r *Runtime) Observe(event trafficevidence.TrafficEvent) {
	if r == nil {
		return
	}
	select {
	case r.commands <- runtimeCommand{event: event}:
	default:
	}
}

// Close signals the actor to stop and waits. Idempotent.
func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.cancel()
	<-r.done
}

// InspectJSON returns the exact pending report, or the current active snapshot,
// without transmitting or mutating telemetry state.
func (r *Runtime) InspectJSON(ctx context.Context) ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	reply := make(chan inspectResponse, 1)
	select {
	case r.commands <- runtimeCommand{inspect: reply}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case result := <-reply:
		return result.body, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type inspectResponse struct {
	body []byte
	err  error
}

type runtimeCommand struct {
	event   trafficevidence.TrafficEvent
	inspect chan<- inspectResponse
}

// runtimeState is the goroutine-local actor state: only the run goroutine reads
// and writes these fields, so they carry no mutex.
type runtimeState struct {
	store           store
	uploader        *reportUploader
	version         string
	logger          *slog.Logger
	now             func() time.Time
	identity        identity
	preference      preference
	reducer         *reportReducer
	pending         *productReport
	debug           bool
	cadence         time.Duration
	firstFlushDelay time.Duration
	uploadTimeout   time.Duration
}

func (s *runtimeState) run(ctx context.Context, commands <-chan runtimeCommand, done chan<- struct{}) {
	defer func() {
		if recovered := recover(); recovered != nil && s.logger != nil {
			s.logger.Error("product telemetry disabled after internal panic", "panic", recovered)
		}
		close(done)
	}()
	uploadTick := time.NewTicker(s.cadence)
	defer uploadTick.Stop()
	prefTick := time.NewTicker(preferencePollInterval)
	defer prefTick.Stop()
	var firstFlush <-chan time.Time
	var firstTimer *time.Timer
	for {
		select {
		case <-ctx.Done():
			s.drain(commands)
			s.flush()
			return
		case command := <-commands:
			if command.inspect != nil {
				command.inspect <- s.inspect()
				continue
			}
			if s.preference.Enabled {
				s.reducer.Observe(command.event)
				if firstTimer == nil {
					firstTimer = time.NewTimer(s.firstFlushDelay)
					firstFlush = firstTimer.C
				}
			}
		case <-firstFlush:
			s.flush()
			firstFlush = nil
		case <-prefTick.C:
			// Adopt external preference changes promptly. refreshPreference resets
			// the reducer when the revision changes (discarding any aggregate
			// collected under a since-revoked preference).
			s.refreshPreference()
		case <-uploadTick.C:
			s.flush()
		}
	}
}

func (s *runtimeState) drain(commands <-chan runtimeCommand) {
	for {
		select {
		case command := <-commands:
			if command.inspect != nil {
				command.inspect <- s.inspect()
			} else if s.preference.Enabled {
				s.reducer.Observe(command.event)
			}
		default:
			return
		}
	}
}

// refreshPreference reads the persisted preference, discarding the reducer's
// aggregate when the revision changed or the preference could not be read safely
// (fail closed). It returns the current enabled state.
func (s *runtimeState) refreshPreference() bool {
	pref, err := s.store.preference()
	if err != nil {
		s.preference = preference{Enabled: false}
		s.reducer.Reset()
		return false
	}
	if pref.Revision != s.preference.Revision {
		s.reducer.Reset()
		s.pending = nil
	}
	s.preference = pref
	return pref.Enabled
}

// flush is the single method that may transmit. It freezes active aggregation
// into one immutable pending report, then clears pending only after acceptance.
func (s *runtimeState) flush() {
	if !s.refreshPreference() {
		return
	}
	if s.pending == nil {
		if s.reducer.Empty() {
			return
		}
		report, err := s.buildReport(true)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("product telemetry report construction failed", "error", err.Error())
			}
			return
		}
		s.pending = &report
		s.reducer.Reset()
	}
	if s.debug {
		if body, err := json.Marshal(s.pending); err == nil && s.logger != nil {
			s.logger.Info("product telemetry debug report", "report", string(body))
		}
		s.pending = nil
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.uploadTimeout)
	defer cancel()
	if err := s.uploader.Upload(ctx, *s.pending); err != nil {
		if s.logger != nil {
			s.logger.Warn("product telemetry upload failed", "error", err.Error())
		}
		return
	}
	s.pending = nil
}

func (s *runtimeState) inspect() inspectResponse {
	if !s.preference.Enabled {
		return inspectResponse{body: []byte("null")}
	}
	if s.pending != nil {
		body, err := json.Marshal(s.pending)
		return inspectResponse{body: body, err: err}
	}
	if s.reducer.Empty() {
		return inspectResponse{body: []byte("null")}
	}
	report, err := s.buildInspectionReport()
	if err != nil {
		return inspectResponse{body: []byte("null")}
	}
	body, err := json.Marshal(report)
	return inspectResponse{body: body, err: err}
}

func (s *runtimeState) buildInspectionReport() (productReport, error) {
	report, err := s.buildReport(false)
	if err != nil {
		return productReport{}, err
	}
	report.ReportCreatedAt = s.now().UTC().Format(time.RFC3339)
	content, err := json.Marshal(report)
	if err != nil {
		return productReport{}, err
	}
	digest := sha256.Sum256(content)
	report.ReportID = fmt.Sprintf("%x", digest[:16])
	return report, nil
}

// buildReport snapshots the reducer and overlays installation age from the cached
// identity (no disk read). There is no first-success overlay: activation is
// derived downstream from the earliest accepted successful report.
func (s *runtimeState) buildReport(freeze bool) (productReport, error) {
	report := s.reducer.snapshot(s.identity.InstallID, s.version, goruntime.GOOS, goruntime.GOARCH)
	if freeze {
		reportID, err := newToken()
		if err != nil {
			return productReport{}, err
		}
		report.ReportID = reportID
		report.ReportCreatedAt = s.now().UTC().Format(time.RFC3339)
	}
	if firstSeen, err := time.Parse(time.RFC3339, s.identity.FirstSeenAt); err == nil {
		report.InstallationAgeBucket = AgeBucket(s.now().Sub(firstSeen))
	}
	return report, nil
}

func logDisabled(logger *slog.Logger, what string, err error) {
	if logger != nil {
		logger.Warn("product telemetry disabled for this lifetime: "+what+" unreadable", "error", err.Error())
	}
}

func resolveReportEndpoint() string {
	base := strings.TrimRight(platformconfig.ResolveTelemetryEndpoint(embeddedTelemetryEndpoint), "/")
	return base + reportEndpointPath
}
