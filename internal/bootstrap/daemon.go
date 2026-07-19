// dependency wiring, and control-plane serving in one process seam.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	credentialsadapter "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	providersadapter "github.com/swobuforge/swobu/internal/adapters/outbound/providers"
	trafficevidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/trafficevidence"
	"github.com/swobuforge/swobu/internal/configstore"
	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/telemetry"
)

type HealthState string

const (
	HealthStateUninitialized HealthState = "uninitialized"
	HealthStateHealthy       HealthState = "healthy"
	HealthStateDegraded      HealthState = "degraded"
)

// Status is the first machine-readable runtime health projection. CLI and TUI
// surfaces can render or relay it without re-deriving state from prose.
type Status struct {
	State          HealthState `json:"state"`
	WorkspaceCount int         `json:"workspace_count"`
}

// Daemon is the live process boundary produced by bootstrap. It owns listener
// lifetime, runtime health, and graceful shutdown for the local daemon.
type Daemon struct {
	configStore       *configstore.Store
	server            *http.Server
	listener          net.Listener
	logger            *slog.Logger
	done              chan struct{}
	closeOnce         sync.Once
	serveErr          error
	serveErrMu        sync.Mutex
	trafficEventStore *trafficevidencestore.InMemoryTrafficEventSink
	telemetry         embeddedTelemetryRuntimeState
}

var daemonReadHeaderTimeout = 10 * time.Second
var daemonReadTimeout = 30 * time.Second
var daemonWriteTimeout = 5 * time.Minute
var daemonIdleTimeout = 60 * time.Second

// StartInput collects the one runtime config path plus the dependencies
// bootstrap must wire into the live request path.
type StartInput struct {
	ConfigPath       string
	StartupConfig    config.StartupConfig
	Providers        provider.BackendResolver
	ModelCatalog     provider.Discovery
	TrafficEventSink observation.TrafficEventSink
	Logger           *slog.Logger
}

// operator routes, and request-path dependencies in one bootstrap flow.
func Start(ctx context.Context, in StartInput) (*Daemon, error) {
	logger := in.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("daemon lifecycle", "component", "daemon", "event", "intent_store_open_start", "config_path", in.ConfigPath)
	store, err := configstore.OpenOrCreate(in.ConfigPath)
	if err != nil {
		logger.Error("daemon lifecycle", "component", "daemon", "event", "intent_store_open_failed", "config_path", in.ConfigPath, "error", err.Error())
		return nil, err
	}
	storeOwnedByDaemon := false
	defer func() {
		if !storeOwnedByDaemon {
			_ = store.Close()
		}
	}()
	logger.Info("daemon lifecycle", "component", "daemon", "event", "intent_store_open_success", "config_path", in.ConfigPath, "workspace_count", store.Config().WorkspaceCount())
	startupConfig, err := config.ResolveStartupConfig(in.StartupConfig.Addr)
	if err != nil {
		return nil, fmt.Errorf("resolve daemon address: %w", err)
	}
	addr := startupConfig.Addr

	daemon := &Daemon{
		configStore: store,
		logger:      logger,
		done:        make(chan struct{}),
		telemetry: embeddedTelemetryRuntimeState{
			store: telemetry.NewStore(),
			now:   time.Now,
		},
	}

	if (in.Providers == nil) != (in.ModelCatalog == nil) {
		return nil, fmt.Errorf("provider services must be wired together: providers and discovery must both be set or both be nil")
	}

	providers := in.Providers
	discovery := in.ModelCatalog
	authCredentialWritePolicy := credentialsadapter.NormalizeCredentialWritePolicy(config.ResolveAuthCredentialWritePolicy())
	logger.Info("auth credential policy resolved",
		"component", "daemon",
		"write_policy", string(authCredentialWritePolicy),
	)
	if providers == nil {
		// Bootstrap owns provider wiring composition so operator surfaces do not
		// import provider adapters directly.
		composition, err := providersadapter.NewProviderRegistry(
			newProviderHTTPClient(),
			credentialsadapter.NewResolver(),
		)
		if err != nil {
			return nil, fmt.Errorf("compose providers: %w", err)
		}
		providers = composition
		discovery = composition
	}
	runtimeRoot := newDaemonProviderModelCatalogComposition(codecresolver.NewRuntimeCodecResolver(), providers, discovery)
	trafficEventSink := in.TrafficEventSink
	if trafficEventSink == nil {
		daemon.trafficEventStore = trafficevidencestore.NewTrafficEventStore(trafficevidencestore.StoreConfig{})
		trafficEventSink = daemon.trafficEventStore
	} else if store, ok := trafficEventSink.(*trafficevidencestore.InMemoryTrafficEventSink); ok {
		daemon.trafficEventStore = store
	}
	trafficEventSink = newTelemetryObservedTrafficEventSink(trafficEventSink, daemon.observeTelemetryEvent)
	mux, chatGPTLogin, err := buildDaemonServeMux(daemon, addr, runtimeRoot, trafficEventSink, authCredentialWritePolicy)
	if err != nil {
		return nil, err
	}
	server := newDaemonHTTPServer(addr, mux)

	logger.Info("daemon lifecycle", "component", "daemon", "event", "bind_start", "addr", addr)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		logger.Error("daemon lifecycle", "component", "daemon", "event", "bind_failure", "addr", addr, "error", err.Error())
		return nil, fmt.Errorf("listen: %w", err)
	}
	logger.Info("daemon lifecycle", "component", "daemon", "event", "bind_success", "addr", listener.Addr().String())
	daemon.server = server
	daemon.listener = listener
	chatGPTLogin.SetPublicBaseURL("http://" + listener.Addr().String())
	go func() {
		defer close(daemon.done)
		err := server.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			daemon.serveErrMu.Lock()
			daemon.serveErr = err
			daemon.serveErrMu.Unlock()
			logger.Error("daemon lifecycle", "component", "daemon", "event", "serve_failure", "error", err.Error())
		}
	}()
	logger.Info("daemon lifecycle", "component", "daemon", "event", "initialization_completed", "addr", listener.Addr().String())
	daemon.startTelemetryRuntime()
	storeOwnedByDaemon = true

	return daemon, nil
}

func (d *Daemon) Close(ctx context.Context) error {
	if d == nil || d.server == nil {
		return nil
	}
	if d.logger != nil {
		d.logger.Info("daemon lifecycle", "component", "daemon", "event", "graceful_shutdown_requested")
	}
	var shutdownErr error
	d.closeOnce.Do(func() {
		shutdownErr = d.server.Shutdown(ctx)
	})
	if shutdownErr != nil {
		if d.logger != nil {
			d.logger.Error("daemon lifecycle", "component", "daemon", "event", "graceful_shutdown_failed", "error", shutdownErr.Error())
		}
		return shutdownErr
	}
	d.stopTelemetryRuntimeWithContext(ctx)
	if d.configStore != nil {
		if err := d.configStore.Close(); err != nil {
			return err
		}
	}
	select {
	case <-d.done:
		serveErr := d.serveError()
		if serveErr != nil {
			if d.logger != nil {
				d.logger.Error("daemon lifecycle", "component", "daemon", "event", "graceful_shutdown_failed", "error", serveErr.Error())
			}
			return serveErr
		}
		if d.logger != nil {
			d.logger.Info("daemon lifecycle", "component", "daemon", "event", "graceful_shutdown_completed")
		}
		return nil
	case <-ctx.Done():
		if d.logger != nil {
			d.logger.Warn("daemon lifecycle", "component", "daemon", "event", "graceful_shutdown_timed_out", "error", ctx.Err().Error())
		}
		return ctx.Err()
	}
}

func (d *Daemon) Addr() string {
	if d == nil || d.listener == nil {
		return ""
	}
	return d.listener.Addr().String()
}

func (d *Daemon) BaseURL() string {
	addr := d.Addr()
	if addr == "" {
		return ""
	}
	return config.BaseURL(addr)
}

func (d *Daemon) Status() (Status, error) {
	if d == nil {
		return Status{}, fmt.Errorf("daemon is nil")
	}
	status := Status{
		State:          HealthStateHealthy,
		WorkspaceCount: d.configStore.Config().WorkspaceCount(),
	}
	if status.WorkspaceCount == 0 {
		status.State = HealthStateUninitialized
		return status, nil
	}
	if d.isRequestPathDegraded() {
		status.State = HealthStateDegraded
	}
	return status, nil
}

func (d *Daemon) isRequestPathDegraded() bool {
	if d == nil || d.trafficEventStore == nil {
		return false
	}
	projection := d.trafficEventStore.ProjectStatus(trafficevidencestore.ProjectionInput{
		State:          string(HealthStateHealthy),
		WorkspaceCount: d.configStore.Config().WorkspaceCount(),
		Scope:          trafficevidencestore.ProjectionScope{Kind: trafficevidencestore.ProjectionScopeAll},
	})
	for _, row := range projection.RecentTraffic {
		resultClass, err := trafficevidence.ParseResultClass(row.Result)
		if err != nil || !resultClass.IsTerminal() {
			continue
		}
		if resultClass != trafficevidence.ResultClassSuccess && resultClass != trafficevidence.ResultClassCancelled {
			return true
		}
	}
	return false
}

func (d *Daemon) StatusProjectionForScope(scope trafficevidencestore.ProjectionScope) (trafficevidencestore.StatusProjection, error) {
	status, err := d.Status()
	if err != nil {
		return trafficevidencestore.StatusProjection{}, err
	}
	if d.trafficEventStore == nil {
		return trafficevidencestore.StatusProjection{
			State:          string(status.State),
			WorkspaceCount: status.WorkspaceCount,
			Scope:          scope,
			Counters: trafficevidencestore.StatusCounters{
				PerModel: map[string]int{},
			},
		}, nil
	}
	return d.trafficEventStore.ProjectStatus(trafficevidencestore.ProjectionInput{
		State:          string(status.State),
		WorkspaceCount: status.WorkspaceCount,
		Scope:          scope,
	}), nil
}

func (d *Daemon) Wait(ctx context.Context) error {
	if d == nil {
		return fmt.Errorf("daemon is nil")
	}
	select {
	case <-d.done:
		return d.serveError()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Daemon) serveError() error {
	d.serveErrMu.Lock()
	defer d.serveErrMu.Unlock()
	return d.serveErr
}
