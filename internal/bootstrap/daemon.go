// dependency wiring, and control-plane serving in one process seam.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/accounting"
	credentialsadapter "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	executionevidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	providersadapter "github.com/swobuforge/swobu/internal/adapters/outbound/providers"
	"github.com/swobuforge/swobu/internal/app/operator/adminapproval"
	"github.com/swobuforge/swobu/internal/app/operator/chatgptlogin"
	connectionapp "github.com/swobuforge/swobu/internal/app/operator/connections"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/producttelemetry"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/publications"
	"github.com/swobuforge/swobu/internal/relaystate"
	"github.com/swobuforge/swobu/internal/relaytransport"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/search"
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
// Close owns cleanup: it drains HTTP handlers, retires the independent ChatGPT
// callback listener and pending browser sessions, joins credential maintenance,
// then stops telemetry and
// closes the stores. Cleanup therefore never overlaps an in-flight request.
// There is no retryable shutdown state machine and no force-close mode: a handler
// that ignores cancellation will block Close, which is the process supervisor's
// boundary to break. Close runs once and returns the cached terminal result.
type Daemon struct {
	nodeFile                  *nodefile.File
	configStore               *configstore.Store
	connectionQueries         *connectionapp.Queries
	connectionStore           *connections.Store
	connectionLifecycle       *connections.Lifecycle
	stopConnectionMaintenance func()
	relayStore                *relaystate.Store
	accessStore               *accessstate.Store
	publicationStore          *publications.Store
	accountingStore           *accounting.Store
	relayRuntime              *relaytransport.OwnerRuntime
	publicHandler             http.Handler
	server                    *http.Server
	listener                  net.Listener
	logger                    *slog.Logger
	serveDone                 chan struct{}
	serveErr                  error
	closeOnce                 sync.Once
	closeErr                  error
	executionEvidenceStore    *executionevidencestore.Store
	telemetry                 *producttelemetry.Runtime
	configPath                string
	runtimeDir                string
	adminMCPBinding           mcp.SourceBinding
	mcpBindings               map[string]mcp.SourceBinding
	mcpBindingsMu             sync.Mutex
	searchService             search.Service
	searchEvidence            *search.EvidenceStore
	chatGPTLogin              *chatgptlogin.LoginService
	adminApprovals            *adminapproval.Service
	ledgerStore               ledger.Store
	ledgerWriter              *ledger.Writer
	ledgerOpenFailed          bool
	pricing                   *daemonPricing
}

var daemonReadHeaderTimeout = 10 * time.Second
var daemonReadTimeout = 30 * time.Second
var daemonIdleTimeout = 60 * time.Second

// StartInput collects the one runtime config path plus the dependencies
// bootstrap must wire into the live request path.
type StartInput struct {
	ConfigPath string
	// ReadOnlyConfig uses existing externally managed Node YAML; config changes
	// fail while normal Workspace inference and runtime evidence may continue.
	ReadOnlyConfig bool
	StartupConfig  config.StartupConfig
	Providers      provider.BackendResolver
	ModelCatalog   provider.Discovery
	Logger         *slog.Logger
	// MCPConnectionBindings substitutes transports for registered Connections in tests.
	// Bindings contain closures, never persisted secrets.
	MCPConnectionBindings map[string]mcp.SourceBinding
	SearchService         search.Service
}

// operator routes, and request-path dependencies in one bootstrap flow.
func Start(ctx context.Context, in StartInput) (*Daemon, error) {
	logger := in.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("daemon lifecycle", "component", "daemon", "event", "intent_store_open_start", "config_path", in.ConfigPath)
	store, err := openDaemonRoutingConfigured(ctx, in.ConfigPath, in.ReadOnlyConfig)
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
	connectionQueries, connectionStore, connectionLifecycle, node, err := openDaemonConnections(ctx, logger, in.ConfigPath, store)
	if err != nil {
		return nil, fmt.Errorf("open Connections: %w", err)
	}

	runtimeDir, err := config.NodeRuntimeRoot(in.ConfigPath, in.ReadOnlyConfig)
	if err != nil {
		return nil, err
	}
	if err := config.ValidateNodeRuntimeOwnership(in.ConfigPath, in.ReadOnlyConfig, runtimeDir); err != nil {
		return nil, err
	}
	relayStore, err := relaystate.Open(filepath.Join(runtimeDir, "relay.json"))
	if err != nil {
		return nil, err
	}
	principalRaw, err := node.PrincipalDocument()
	if err != nil {
		return nil, err
	}
	accessStore, err := accessstate.OpenInline(principalRaw, node.PublishPrincipals)
	if err != nil {
		return nil, fmt.Errorf("open Principal authority: %w", err)
	}
	publicationRaw, err := node.PublicationDocument()
	if err != nil {
		return nil, err
	}
	publicationStore, err := publications.OpenInline(publicationRaw, node.PublishPublications)
	if err != nil {
		return nil, fmt.Errorf("open Workspace publication: %w", err)
	}
	accountingRaw, err := node.AccountingDocument()
	if err != nil {
		return nil, fmt.Errorf("read accounting policy: %w", err)
	}
	// Admission must also protect externally managed YAML: no managed writer
	// may ever run to catch dangling budget references before serving traffic.
	limits, err := accounting.ValidatePolicyDocument(accountingRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid Node accounting policy: %w", err)
	}
	for name := range limits {
		slug, slugErr := routing.ParseWorkspaceSlug(name)
		if slugErr != nil {
			return nil, fmt.Errorf("accounting.limits.%s: %w", name, slugErr)
		}
		if _, present := store.Config().Workspace(slug); !present {
			return nil, fmt.Errorf("accounting.limits.%s: referenced Workspace is absent", name)
		}
	}
	accountingStore, err := accounting.OpenPolicyInline(filepath.Join(runtimeDir, "accounting.json"), accountingRaw, node.PublishAccounting)
	if err != nil {
		return nil, err
	}
	startupConfig, err := config.ResolveStartupConfig(in.StartupConfig.Addr)
	if err != nil {
		return nil, fmt.Errorf("resolve daemon address: %w", err)
	}
	addr := startupConfig.Addr
	resolver := credentialsadapter.NewResolver()
	// MCP runtime bindings are built only from typed Connections or explicit
	// in-memory test seams; the detached source registry is no longer opened.
	mcpBindings := make(map[string]mcp.SourceBinding)
	for _, value := range connectionStore.List() {
		if _, injected := in.MCPConnectionBindings[value.ID().String()]; injected {
			// Deterministic test seams provide the transport for this typed
			// Connection; do not open a second live session to its fixture endpoint.
			continue
		}
		var binding mcp.SourceBinding
		var bindErr error
		resolve := func(resolveCtx context.Context, scope credentialref.Scope, locator string) (string, error) {
			return resolver.ResolveScopedCredential(resolveCtx, scope, locator)
		}
		switch value.Spec().(type) {
		case connections.MCPHTTPSpec:
			binding, bindErr = mcp.OpenHTTPConnectionBinding(ctx, value, resolve)
		case connections.MCPStdioSpec:
			binding, bindErr = mcp.OpenStdioConnectionBinding(ctx, value, resolve)
		default:
			continue
		}
		if bindErr != nil {
			logger.Warn("MCP Connection unavailable; Workspace mounts will fail closed", "component", "daemon", "event", "mcp_connection_unavailable", "connection_id", value.ID().String())
			continue
		}
		mcpBindings[value.ID().String()] = binding
	}
	bindingsOwned := false
	defer func() {
		if !bindingsOwned {
			closeMCPBindings(mcpBindings)
		}
	}()
	for id, binding := range in.MCPConnectionBindings {
		parsed, err := connectionidentity.Parse(id)
		if err != nil || parsed.String() != id {
			return nil, fmt.Errorf("injected MCP binding has invalid Connection identity %q", id)
		}
		connection, err := connectionStore.Get(parsed)
		if err != nil {
			return nil, fmt.Errorf("injected MCP binding %q has no registered Connection: %w", id, err)
		}
		switch connection.Spec().(type) {
		case connections.MCPHTTPSpec, connections.MCPStdioSpec:
		default:
			return nil, fmt.Errorf("injected MCP binding %q references a non-MCP Connection", id)
		}
		if binding.ID != id || binding.ConnectionID != id || binding.ConnectionRevision != connection.Revision().String() {
			return nil, fmt.Errorf("injected MCP binding %q does not match the registered Connection revision", id)
		}
		mcpBindings[id] = binding
	}

	daemon := &Daemon{
		nodeFile:            node,
		configStore:         store,
		connectionQueries:   connectionQueries,
		connectionStore:     connectionStore,
		connectionLifecycle: connectionLifecycle,
		relayStore:          relayStore,
		accessStore:         accessStore,
		accountingStore:     accountingStore,
		publicationStore:    publicationStore,
		logger:              logger,
		serveDone:           make(chan struct{}),
		configPath:          in.ConfigPath,
		runtimeDir:          runtimeDir,
		mcpBindings:         cloneMCPBindings(mcpBindings),
		searchService:       in.SearchService,
		adminApprovals:      adminapproval.New(nil),
	}
	// Direct Search keeps its existing bounded evidence head in the canonical
	// runtime directory, independently of query bodies and Exchange/MCP history.
	searchEvidenceName := "search-evidence.json"
	if !store.ReadOnly() {
		searchEvidenceName = strings.TrimSuffix(filepath.Base(in.ConfigPath), filepath.Ext(in.ConfigPath)) + ".search-evidence.json"
	}
	daemon.searchEvidence, err = search.OpenEvidenceStore(filepath.Join(runtimeDir, searchEvidenceName), 512)
	if err != nil {
		logger.Warn("Search evidence retention unavailable", "component", "search", "event", "evidence_retention_unavailable")
	}
	defer func() {
		if !storeOwnedByDaemon {
			_ = daemon.searchEvidence.Close()
		}
	}()
	// Direct Search shares Workspace budget policy on local and Owner ingress.
	// An unpriced Connection remains unknown; publication cannot bypass its limit.
	daemon.searchService.Accounting = accountingStore
	daemon.searchService.Evidence = daemon.searchEvidence.Append
	base := daemon.searchService
	if base.ConnectionIDs == nil {
		base.ConnectionIDs = func(rawWorkspace string) ([]string, bool) {
			slug, err := routing.ParseWorkspaceSlug(rawWorkspace)
			if err != nil {
				return nil, false
			}
			workspace, ok := store.Config().Workspace(slug)
			if !ok || !workspace.HasService(routing.ServiceSearch) {
				return nil, false
			}
			ids := workspace.SearchConnections()
			return ids, len(ids) > 0
		}
	}
	if base.SourceFor == nil && len(base.Sources) == 0 {
		// Resolve every new attempt from current Connection authority; no stale
		// credential or startup-only source registry survives an edit/revocation.
		base.SourceFor = func(ctx context.Context, rawID string) (search.Source, error) {
			id, err := connectionidentity.Parse(rawID)
			if err != nil {
				return nil, search.ErrNotConfigured
			}
			value, err := connectionStore.Get(id)
			if err != nil {
				return nil, search.ErrNotConfigured
			}
			resolver := credentialsadapter.NewResolver()
			bound, err := search.OpenConnectionBinding(ctx, value, resolver.ResolveScopedCredential, nil)
			if err != nil {
				return nil, search.ErrNotConfigured
			}
			current, err := connectionStore.Get(id)
			if err != nil || current.Revision() != value.Revision() {
				return nil, search.ErrNotConfigured
			}
			return bound, nil
		}
	}
	daemon.searchService = base

	if (in.Providers == nil) != (in.ModelCatalog == nil) {
		return nil, fmt.Errorf("provider services must be wired together: providers and discovery must both be set or both be nil")
	}

	providers := in.Providers
	discovery := in.ModelCatalog
	authCredentialWritePolicy, policyErr := credentialsadapter.ParseCredentialWritePolicy(config.ResolveAuthCredentialWritePolicy())
	if policyErr != nil {
		return nil, fmt.Errorf("resolve credential write policy: %w", policyErr)
	}
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
	daemon.pricing, err = openDaemonPricing(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !storeOwnedByDaemon && daemon.pricing != nil {
			_ = daemon.pricing.reader.Close()
		}
	}()
	if err := daemon.startLedger(ctx); err != nil {
		return nil, err
	}
	defer func() {
		if !storeOwnedByDaemon && daemon.ledgerWriter != nil {
			_ = daemon.ledgerWriter.Close()
		}
	}()
	evidenceConfig := executionevidencestore.Config{}
	if daemon.ledgerWriter != nil {
		evidenceConfig.OfferMCP = daemon.ledgerWriter.OfferMCP
		evidenceConfig.OfferExchange = daemon.ledgerWriter.Offer
	}
	daemon.executionEvidenceStore = executionevidencestore.NewStore(evidenceConfig)
	logger.Info("daemon lifecycle", "component", "daemon", "event", "bind_start", "addr", addr)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		logger.Error("daemon lifecycle", "component", "daemon", "event", "bind_failure", "addr", addr, "error", err.Error())
		return nil, fmt.Errorf("listen: %w", err)
	}
	logger.Info("daemon lifecycle", "component", "daemon", "event", "bind_success", "addr", listener.Addr().String())
	mux, chatGPTLogin, err := buildDaemonServeMux(daemon, listener.Addr().String(), runtimeRoot, authCredentialWritePolicy)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	server := newDaemonHTTPServer(listener.Addr().String(), mux)
	daemon.server = server
	daemon.listener = listener
	daemon.chatGPTLogin = chatGPTLogin
	chatGPTLogin.SetPublicBaseURL("http://" + listener.Addr().String())
	logger.Info("daemon lifecycle", "component", "daemon", "event", "initialization_completed", "addr", listener.Addr().String())
	// Start every owned subsystem before exposing the server goroutine, so a fast
	// serve failure cannot run cleanup against subsystems that have not started.
	daemon.startTelemetryRuntime()
	if !store.ReadOnly() {
		daemon.startConnectionMaintenance()
	}
	go daemon.serve()
	storeOwnedByDaemon = true
	bindingsOwned = true

	return daemon, nil
}

func cloneMCPBindings(in map[string]mcp.SourceBinding) map[string]mcp.SourceBinding {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]mcp.SourceBinding, len(in))
	for id, binding := range in {
		out[id] = binding
	}
	return out
}

func (d *Daemon) currentMCPBindings(sourceIDs ...string) map[string]mcp.SourceBinding {
	if d == nil {
		return nil
	}
	d.refreshConnectionBindings(sourceIDs...)
	d.mcpBindingsMu.Lock()
	defer d.mcpBindingsMu.Unlock()
	bindings := cloneMCPBindings(d.mcpBindings)
	if d.adminMCPBinding.Call != nil {
		if bindings == nil {
			bindings = make(map[string]mcp.SourceBinding)
		}
		bindings[d.adminMCPBinding.ID] = d.adminMCPBinding
	}
	return bindings
}

// workspaceMCPBindings never discovers unrelated Connections. In particular an
// unreachable, unmounted upstream must not prevent builtin administration from
// deleting it. The builtin ID also makes an empty Workspace an explicit scope.
func (d *Daemon) workspaceMCPBindings(workspace routing.Workspace) map[string]mcp.SourceBinding {
	ids := []string{"builtin:" + routing.AdminMCPSource}
	for _, mount := range workspace.MCPMounts() {
		ids = append(ids, mount.SourceID())
	}
	return d.currentMCPBindings(ids...)
}

// refreshConnectionBindings derives HTTP/stdio sessions from current MCP
// Connections. A revision change or deletion invalidates the old session before
// reopening; a failed open leaves no callable binding and can recover on a
// later request. No previous credential or session is a fallback authority.
func (d *Daemon) refreshConnectionBindings(sourceIDs ...string) {
	if d == nil || d.connectionStore == nil {
		return
	}
	d.mcpBindingsMu.Lock()
	defer d.mcpBindingsMu.Unlock()
	if d.mcpBindings == nil {
		d.mcpBindings = make(map[string]mcp.SourceBinding)
	}
	resolver := credentialsadapter.NewResolver()
	resolve := func(ctx context.Context, scope credentialref.Scope, locator string) (string, error) {
		return resolver.ResolveScopedCredential(ctx, scope, locator)
	}
	values := d.connectionStore.List()
	live := make(map[string]connections.Connection, len(values))
	for _, value := range values {
		if value.Spec().Kind() == connections.MCP {
			live[value.ID().String()] = value
		}
	}
	for id, binding := range d.mcpBindings {
		value, exists := live[id]
		if exists && binding.ConnectionRevision == value.Revision().String() {
			continue
		}
		// Close before attempting replacement: discovery failure must not leave
		// a revoked credential or deleted Connection available for dispatch.
		delete(d.mcpBindings, id)
		if binding.Close != nil {
			_ = binding.Close()
		}
	}
	requested := make(map[string]bool, len(sourceIDs))
	for _, id := range sourceIDs {
		requested[id] = true
	}
	for _, value := range values {
		id := value.ID().String()
		if _, exists := d.mcpBindings[id]; exists || value.Spec().Kind() != connections.MCP || (len(sourceIDs) > 0 && !requested[id]) {
			continue
		}
		var next mcp.SourceBinding
		var err error
		switch value.Spec().(type) {
		case connections.MCPHTTPSpec:
			next, err = mcp.OpenHTTPConnectionBinding(context.Background(), value, resolve)
		case connections.MCPStdioSpec:
			next, err = mcp.OpenStdioConnectionBinding(context.Background(), value, resolve)
		}
		if err != nil {
			continue
		}
		// Opening performs I/O without a registry lock. Do not install a session
		// whose Connection changed or disappeared during that discovery.
		current, err := d.connectionStore.Get(value.ID())
		if err != nil || current.Revision() != value.Revision() {
			if next.Close != nil {
				_ = next.Close()
			}
			continue
		}
		d.mcpBindings[id] = next
	}
}

func closeMCPBindings(bindings map[string]mcp.SourceBinding) {
	for _, binding := range bindings {
		if binding.Close != nil {
			_ = binding.Close()
		}
	}
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
		var authErr error
		if d.chatGPTLogin != nil {
			authErr = d.chatGPTLogin.Shutdown(context.Background())
		}
		if d.stopConnectionMaintenance != nil {
			d.stopConnectionMaintenance()
		}
		d.stopTelemetryRuntime()
		// Shutdown has drained every handler that can reach a binding, so its
		// remote SDK sessions can now be closed before dependent state stores.
		closeMCPBindings(d.mcpBindings)
		if d.relayRuntime != nil {
			d.relayRuntime.Stop()
		}
		var storeErr error
		var ledgerErr error
		if d.ledgerWriter != nil {
			ledgerErr = d.ledgerWriter.Close()
		}
		if d.configStore != nil {
			storeErr = d.configStore.Close()
		}
		searchEvidenceErr := d.searchEvidence.Close()
		var pricingErr error
		if d.pricing != nil {
			pricingErr = d.pricing.reader.Close()
		}
		d.closeErr = errors.Join(shutdownErr, d.serveErr, authErr, storeErr, ledgerErr, pricingErr, searchEvidenceErr)
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
	}
	if d.ledgerOpenFailed || d.ledgerWriter != nil && d.ledgerWriter.Health().State == ledger.StateDegraded || d.searchEvidence != nil && d.searchEvidence.RetentionError() {
		status.State = HealthStateDegraded
	}
	return status, nil
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
