// dependency wiring, and control-plane serving in one process seam.
package bootstrap

import (
	"context"
	"errors"
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
	"github.com/swobuforge/swobu/internal/producttelemetry"
	"github.com/swobuforge/swobu/internal/provider"
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
//
// Close owns cleanup: it drives http.Server.Shutdown (which stops accepting and
// waits for active handlers to finish before returning), then stops telemetry and
// closes the config store. Cleanup therefore never overlaps an in-flight request.
// There is no retryable shutdown state machine and no force-close mode: a handler
// that ignores cancellation will block Close, which is the process supervisor's
// boundary to break. Close runs once and returns the cached terminal result.
type Daemon struct {
	configStore       *configstore.Store
	server            *http.Server
	listener          net.Listener
	logger            *slog.Logger
	serveDone         chan struct{}
	serveErr          error
	closeOnce         sync.Once
	closeErr          error
	trafficEventStore *trafficevidencestore.InMemoryTrafficEventSink
	telemetry         *producttelemetry.Runtime
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
	TargetSupport    provider.TargetSupportResolver
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
		serveDone:   make(chan struct{}),
	}

	if (in.Providers == nil) != (in.ModelCatalog == nil) || (in.Providers == nil) != (in.TargetSupport == nil) {
		return nil, fmt.Errorf("provider services must be wired together: providers, target support, and discovery must all be set or all be nil")
	}

	providers := in.Providers
	targetSupport := in.TargetSupport
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
		targetSupport = composition
		discovery = composition
	}
	runtimeRoot := newDaemonProviderModelCatalogComposition(codecresolver.NewRuntimeCodecResolver(), providers, targetSupport, discovery)
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
	logger.Info("daemon lifecycle", "component", "daemon", "event", "initialization_completed", "addr", listener.Addr().String())
	// Start every owned subsystem before exposing the server goroutine, so a fast
	// serve failure cannot run cleanup against subsystems that have not started.
	daemon.startTelemetryRuntime()
	go daemon.serve()
	storeOwnedByDaemon = true

	return daemon, nil
}

// serve runs the HTTP server until it exits and publishes the serve result. It
// performs no cleanup — Close owns that, because Serve can return (with
// http.ErrServerClosed) while Shutdown is still draining active handlers, so
// request-owned dependencies must not close until Shutdown returns. Closing
// serveDone publishes serveErr with the necessary memory ordering.
func (d *Daemon) serve() {
	err := d.server.Serve(d.listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	} else if err != nil && d.logger != nil {
		d.logger.Error("daemon lifecycle", "component", "daemon", "event", "serve_failure", "error", err.Error())
	}
	d.serveErr = err
	close(d.serveDone)
}

// Close stops request serving before closing request-owned dependencies. Shutdown
// stops accepting and waits for active handlers to finish; only then are telemetry
// and the config store closed. It runs once and returns the cached terminal
// result (the join of shutdown, serve, and cleanup errors).
func (d *Daemon) Close() error {
	if d == nil || d.server == nil {
		return nil
	}
	d.closeOnce.Do(func() {
		if d.logger != nil {
			d.logger.Info("daemon lifecycle", "component", "daemon", "event", "graceful_shutdown_requested")
		}
		shutdownErr := d.server.Shutdown(context.Background())
		<-d.serveDone
		d.stopTelemetryRuntime()
		var storeErr error
		if d.configStore != nil {
			storeErr = d.configStore.Close()
		}
		d.closeErr = errors.Join(shutdownErr, d.serveErr, storeErr)
		if d.logger != nil {
			if d.closeErr != nil {
				d.logger.Error("daemon lifecycle", "component", "daemon", "event", "graceful_shutdown_failed", "error", d.closeErr.Error())
			} else {
				d.logger.Info("daemon lifecycle", "component", "daemon", "event", "graceful_shutdown_completed")
			}
		}
	})
	return d.closeErr
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
	case <-d.serveDone:
		return d.serveErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
