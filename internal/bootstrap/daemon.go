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

	"github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	"github.com/metrofun/swobu/internal/adapters/outbound/continuitystore"
	credentialsadapter "github.com/metrofun/swobu/internal/adapters/outbound/credentials"
	evidencestore "github.com/metrofun/swobu/internal/adapters/outbound/evidence"
	providersadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers"
	"github.com/metrofun/swobu/internal/app/operator/controlplane"
	operatorendpoints "github.com/metrofun/swobu/internal/app/operator/endpoints"
	operatormodelcatalog "github.com/metrofun/swobu/internal/app/operator/modelcatalog"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/runtimeevidence"
	"github.com/metrofun/swobu/internal/platform/config"
	"github.com/metrofun/swobu/internal/ports"
	"github.com/metrofun/swobu/internal/telemetry"
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
	telemetry  embeddedTelemetryRuntime
}

var providerResponseHeaderTimeout = 5 * time.Minute

// StartInput collects the one runtime config path plus the dependencies
// bootstrap must wire into the live request path.
type StartInput struct {
	ConfigPath string
	Providers  ports.ProviderExecutor
	Evidence   ports.RequestEvidenceSink
	Continuity ports.ResponseContinuityStore
	Logger     *slog.Logger
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
	}

	providers := in.Providers
	if providers == nil {
		// Bootstrap owns provider wiring composition so operator surfaces do not
		// import provider adapters directly.
		providers = providersadapter.NewExecutor(newProviderHTTPClient(), credentialsadapter.NewResolver())
	}
	evidence := in.Evidence
	if evidence == nil {
		daemon.evidence = evidencestore.NewStore(evidencestore.StoreConfig{})
		evidence = daemon.evidence
	} else if store, ok := evidence.(*evidencestore.RequestEvidenceSinkStore); ok {
		daemon.evidence = store
	}
	continuity := in.Continuity
	if continuity == nil {
		// The shipped daemon must own the same continuity semantics as the in-process
		// request path; otherwise responses previous_response_id support would exist
		// only in tests and injected runtimes.
		continuity = continuitystore.NewLocalResponseContinuityStore(continuitystore.LocalResponseContinuityStoreConfig{})
	}

	orchestrator := requestpath.NewRequestHandler(daemon.endpoints, providers, evidence, continuity)
	mux := http.NewServeMux()
	mux.Handle("/c/", httpapi.NewHandler(orchestrator))
	// Status is rendered at the HTTP edge for the same reason request handling is:
	// bootstrap owns runtime truth, while httpapi owns HTTP response shape.
	mux.Handle("/_swobu/status", httpapi.NewStatusHandler(func(context.Context) (httpapi.StatusDocument, error) {
		status, err := daemon.Status()
		if err != nil {
			return httpapi.StatusDocument{}, err
		}
		return httpapi.StatusDocument{
			State:                string(status.State),
			EndpointCount:        status.EndpointCount,
			ControlPlaneProtocol: controlplane.Protocol,
			SwobuVersion:         controlplane.SwobuVersion(),
		}, nil
	}))
	mux.Handle("/_swobu/status-projection", httpapi.NewStatusProjectionHandler(func(_ context.Context, scope evidencestore.ProjectionScope) (evidencestore.StatusProjection, error) {
		return daemon.StatusProjectionForScope(scope)
	}))
	// Shutdown is a tiny internal control seam for the daemon CLI. It stays out
	// of the public compatibility contract and only exists so `swobu down` can
	// request graceful stop without inventing a second process manager.
	mux.Handle("/_swobu/down", httpapi.NewShutdownHandler(func(context.Context) error {
		go func() {
			_ = daemon.Close(context.Background())
		}()
		return nil
	}))
	if catalog, ok := providers.(ports.ProviderModelCatalog); ok {
		modelCatalog := operatormodelcatalog.NewLoader(daemon.endpoints, catalog)
		mux.Handle("/_swobu/model-catalog", httpapi.NewModelCatalogHandler(func(ctx context.Context) (operatormodelcatalog.Snapshot, error) {
			return modelCatalog.Load(ctx)
		}))
		modelCatalogPreview := operatormodelcatalog.NewPreviewLoader(catalog)
		mux.Handle("/_swobu/model-catalog/preview", httpapi.NewModelCatalogPreviewHandler(
			func(ctx context.Context, req operatormodelcatalog.PreviewRequest) (operatormodelcatalog.PreviewSnapshot, error) {
				return modelCatalogPreview.Load(ctx, req)
			},
		))
	}
	endpointIntent := operatorendpoints.NewOperatorEndpointStore(daemon.endpoints)
	mux.Handle("/_swobu/endpoints", httpapi.NewEndpointControlHandler(
		func(ctx context.Context) ([]endpointintent.Endpoint, error) { return endpointIntent.List(ctx) },
		func(ctx context.Context, name string) (endpointintent.Endpoint, error) {
			return endpointIntent.Get(ctx, name)
		},
		func(ctx context.Context, endpoint endpointintent.Endpoint) (endpointintent.Endpoint, error) {
			return endpointIntent.Put(ctx, endpoint)
		},
		func(ctx context.Context, name string) error { return endpointIntent.Delete(ctx, name) },
	))
	mux.Handle("/_swobu/endpoints/", httpapi.NewEndpointControlHandler(
		func(ctx context.Context) ([]endpointintent.Endpoint, error) { return endpointIntent.List(ctx) },
		func(ctx context.Context, name string) (endpointintent.Endpoint, error) {
			return endpointIntent.Get(ctx, name)
		},
		func(ctx context.Context, endpoint endpointintent.Endpoint) (endpointintent.Endpoint, error) {
			return endpointIntent.Put(ctx, endpoint)
		},
		func(ctx context.Context, name string) error { return endpointIntent.Delete(ctx, name) },
	))
	server := &http.Server{
		Addr:    cfg.BindAddr,
		Handler: mux,
	}

	logger.Info("daemon lifecycle", "component", "daemon", "event", "bind_start", "bind_addr", cfg.BindAddr)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.BindAddr)
	if err != nil {
		logger.Error("daemon lifecycle", "component", "daemon", "event", "bind_failure", "bind_addr", cfg.BindAddr, "error", err.Error())
		logger.Error("daemon lifecycle", "component", "daemon", "event", "initialization_failed", "error", err.Error())
		return nil, fmt.Errorf("listen: %w", err)
	}
	logger.Info("daemon lifecycle", "component", "daemon", "event", "bind_success", "bind_addr", listener.Addr().String())
	daemon.server = server
	daemon.listener = listener
	daemon.telemetry = embeddedTelemetryRuntime{
		store:            telemetry.NewStore(),
		now:              time.Now,
		projectionSource: daemon,
	}

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

func newProviderHTTPClient() *http.Client {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{}
	}
	transport := baseTransport.Clone()
	transport.ResponseHeaderTimeout = providerResponseHeaderTimeout
	return &http.Client{Transport: transport}
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
