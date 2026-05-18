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

	"github.com/swobuforge/swobu/internal/adapters/outbound/continuitystore"
	credentialsadapter "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	evidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/evidence"
	providersadapter "github.com/swobuforge/swobu/internal/adapters/outbound/providers"
	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/ports"
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
	State         HealthState `json:"state"`
	EndpointCount int         `json:"endpoint_count"`
}

// Daemon is the live process boundary produced by bootstrap. It owns listener
// lifetime, runtime health, and graceful shutdown for the local daemon.
type Daemon struct {
	endpoints  *endpointCatalog
	server     *http.Server
	listener   net.Listener
	logger     *slog.Logger
	done       chan struct{}
	closeOnce  sync.Once
	serveErr   error
	serveErrMu sync.Mutex
	evidence   *evidencestore.RequestEvidenceSinkStore
	telemetry  embeddedTelemetryRuntimeState
}

var daemonReadHeaderTimeout = 10 * time.Second
var daemonReadTimeout = 30 * time.Second
var daemonWriteTimeout = 5 * time.Minute
var daemonIdleTimeout = 60 * time.Second

// StartInput collects the one runtime config path plus the dependencies
// bootstrap must wire into the live request path.
type StartInput struct {
	ConfigPath   string
	Providers    ports.ProviderExecutor
	ModelCatalog ports.ProviderModelCatalog
	Evidence     ports.RequestEvidenceSink
	Continuity   ports.ResponseContinuityStore
	Logger       *slog.Logger
}

// operator routes, and request-path dependencies in one bootstrap flow.
func Start(ctx context.Context, in StartInput) (*Daemon, error) {
	logger := in.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("daemon lifecycle", "component", "daemon", "event", "intent_store_open_start", "config_path", in.ConfigPath)
	loaded, err := config.Load(in.ConfigPath)
	if err != nil {
		logger.Error("daemon lifecycle", "component", "daemon", "event", "intent_store_open_failed", "config_path", in.ConfigPath, "error", err.Error())
		return nil, err
	}
	logger.Info("daemon lifecycle", "component", "daemon", "event", "intent_store_open_success", "config_path", in.ConfigPath, "endpoint_count", len(loaded.Endpoints))
	cfg := loaded.Runtime

	daemon := &Daemon{
		endpoints: newEndpointCatalog(in.ConfigPath, cfg, loaded.Endpoints),
		logger:    logger,
		done:      make(chan struct{}),
		telemetry: embeddedTelemetryRuntimeState{
			store: telemetry.NewStore(),
			now:   time.Now,
		},
	}

	if (in.Providers == nil) != (in.ModelCatalog == nil) {
		return nil, fmt.Errorf("provider services must be wired together: providers and model catalog must both be set or both be nil")
	}

	providers := in.Providers
	modelCatalog := in.ModelCatalog
	authCredentialWritePolicy := credentialsadapter.NormalizeCredentialWritePolicy(config.ResolveAuthCredentialWritePolicy())
	logger.Info("auth credential policy resolved",
		"component", "daemon",
		"write_policy", string(authCredentialWritePolicy),
	)
	if providers == nil {
		// Bootstrap owns provider wiring composition so operator surfaces do not
		// import provider adapters directly.
		services := providersadapter.NewProviderServicesBundle(
			newProviderHTTPClient(),
			credentialsadapter.NewResolver(),
		)
		providers = services.Execution
		modelCatalog = services.ModelCatalog
	}
	evidence := in.Evidence
	if evidence == nil {
		daemon.evidence = evidencestore.NewStore(evidencestore.StoreConfig{})
		evidence = daemon.evidence
	} else if store, ok := evidence.(*evidencestore.RequestEvidenceSinkStore); ok {
		daemon.evidence = store
	}
	evidence = newTelemetryObservedEvidenceSink(evidence, daemon.observeTelemetryEvent)
	continuity := in.Continuity
	if continuity == nil {
		// The shipped daemon must own the same continuity semantics as the in-process
		// request path; otherwise responses previous_response_id support would exist
		// only in tests and injected runtimes.
		continuity = continuitystore.NewLocalResponseContinuityStore(continuitystore.LocalResponseContinuityStoreConfig{})
	}

	mux, chatGPTLogin, err := buildDaemonServeMux(daemon, cfg.BindAddr, providers, modelCatalog, evidence, continuity, authCredentialWritePolicy)
	if err != nil {
		return nil, err
	}
	server := newDaemonHTTPServer(cfg.BindAddr, mux)

	logger.Info("daemon lifecycle", "component", "daemon", "event", "bind_start", "bind_addr", cfg.BindAddr)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.BindAddr)
	if err != nil {
		logger.Error("daemon lifecycle", "component", "daemon", "event", "bind_failure", "bind_addr", cfg.BindAddr, "error", err.Error())
		return nil, fmt.Errorf("listen: %w", err)
	}
	logger.Info("daemon lifecycle", "component", "daemon", "event", "bind_success", "bind_addr", listener.Addr().String())
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
	logger.Info("daemon lifecycle", "component", "daemon", "event", "initialization_completed", "bind_addr", listener.Addr().String())
	daemon.startTelemetryRuntime()

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

func (d *Daemon) BindAddr() string {
	if d == nil || d.listener == nil {
		return ""
	}
	return d.listener.Addr().String()
}

func (d *Daemon) BaseURL() string {
	addr := d.BindAddr()
	if addr == "" {
		return ""
	}
	return "http://" + addr
}

func (d *Daemon) Status() (Status, error) {
	if d == nil {
		return Status{}, fmt.Errorf("daemon is nil")
	}
	status := Status{
		State:         HealthStateHealthy,
		EndpointCount: d.endpoints.Count(),
	}
	if status.EndpointCount == 0 {
		status.State = HealthStateUninitialized
		return status, nil
	}
	if d.isRequestPathDegraded() {
		status.State = HealthStateDegraded
	}
	return status, nil
}

func (d *Daemon) isRequestPathDegraded() bool {
	if d == nil || d.evidence == nil {
		return false
	}
	projection := d.evidence.ProjectStatus(evidencestore.ProjectionInput{
		State:         string(HealthStateHealthy),
		EndpointCount: d.endpoints.Count(),
		Scope:         evidencestore.ProjectionScope{Kind: evidencestore.ProjectionScopeAll},
	})
	for _, row := range projection.RecentTraffic {
		resultClass, err := runtimeevidence.ParseResultClass(row.Result)
		if err != nil || !resultClass.IsTerminal() {
			continue
		}
		if resultClass != runtimeevidence.ResultClassSuccess && resultClass != runtimeevidence.ResultClassCancelled {
			return true
		}
	}
	return false
}

func (d *Daemon) StatusProjection() (evidencestore.StatusProjection, error) {
	return d.StatusProjectionForScope(evidencestore.ProjectionScope{Kind: evidencestore.ProjectionScopeAll})
}

func (d *Daemon) StatusProjectionForScope(scope evidencestore.ProjectionScope) (evidencestore.StatusProjection, error) {
	status, err := d.Status()
	if err != nil {
		return evidencestore.StatusProjection{}, err
	}
	if d.evidence == nil {
		return evidencestore.StatusProjection{
			State:         string(status.State),
			EndpointCount: status.EndpointCount,
			Scope:         scope,
			Counters: evidencestore.StatusCounters{
				PerModel: map[string]int{},
			},
		}, nil
	}
	return d.evidence.ProjectStatus(evidencestore.ProjectionInput{
		State:         string(status.State),
		EndpointCount: status.EndpointCount,
		Scope:         scope,
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
