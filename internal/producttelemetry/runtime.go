package producttelemetry

import (
	"context"
	"log/slog"
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
	observe chan<- trafficevidence.TrafficEvent
	cancel  context.CancelFunc
	done    <-chan struct{}
}

// runtimeConfig is the in-package wiring input. The public StartRuntime constructs
// it from a default store/endpoint/clock; tests build one directly to inject a
// temp-dir store, an endpoint, or a clock.
type runtimeConfig struct {
	store    store
	Version  string
	Endpoint string
	Logger   *slog.Logger
	Now      func() time.Time
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
		store:      cfg.store,
		uploader:   newReportUploader(endpoint),
		version:    cfg.Version,
		logger:     logger,
		now:        now,
		identity:   identity,
		preference: preference,
		reducer:    newReportReducer(),
	}
	events := make(chan trafficevidence.TrafficEvent, 512)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go s.run(ctx, events, done)
	return &Runtime{observe: events, cancel: cancel, done: done}
}

// Observe offers one terminal traffic event to the actor. It never blocks the
// request path: if the buffer is full the event is dropped (telemetry is lossy).
func (r *Runtime) Observe(event trafficevidence.TrafficEvent) {
	if r == nil {
		return
	}
	select {
	case r.observe <- event:
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

// runtimeState is the goroutine-local actor state: only the run goroutine reads
// and writes these fields, so they carry no mutex.
type runtimeState struct {
	store      store
	uploader   *reportUploader
	version    string
	logger     *slog.Logger
	now        func() time.Time
	identity   identity
	preference preference
	reducer    *reportReducer
}

func (s *runtimeState) run(ctx context.Context, events <-chan trafficevidence.TrafficEvent, done chan<- struct{}) {
	defer close(done)
	uploadTick := time.NewTicker(defaultReportCadence)
	defer uploadTick.Stop()
	prefTick := time.NewTicker(preferencePollInterval)
	defer prefTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			if !s.preference.Enabled {
				continue
			}
			s.reducer.Observe(event)
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
	}
	s.preference = pref
	return pref.Enabled
}

// flush is the single method that may transmit. It refreshes the preference,
// then uploads one report only when enabled and non-empty, resetting the reducer
// afterward. An already-running HTTP upload remains the only documented
// exception to "no transmission after a preference change."
func (s *runtimeState) flush() {
	if !s.refreshPreference() {
		return
	}
	if s.reducer.Empty() {
		return
	}
	defer s.reducer.Reset()
	ctx, cancel := context.WithTimeout(context.Background(), defaultUploadTimeout)
	defer cancel()
	if err := s.uploader.Upload(ctx, s.buildReport()); err != nil && s.logger != nil {
		s.logger.Warn("product telemetry upload failed", "error", err.Error())
	}
}

// buildReport snapshots the reducer and overlays installation age from the cached
// identity (no disk read). There is no first-success overlay: activation is
// derived downstream from the earliest accepted successful report.
func (s *runtimeState) buildReport() productReport {
	report := s.reducer.snapshot(s.identity.InstallID, s.version, goruntime.GOOS, goruntime.GOARCH)
	if firstSeen, err := time.Parse(time.RFC3339, s.identity.FirstSeenAt); err == nil {
		report.InstallationAgeBucket = AgeBucket(s.now().Sub(firstSeen))
	}
	return report
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
