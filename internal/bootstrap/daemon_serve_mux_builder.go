package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/inbound/httpapi"
	credentialsadapter "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	executionevidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	executionevidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	ledgerpostgres "github.com/swobuforge/swobu/internal/adapters/outbound/ledger/postgres"
	ledgersqlite "github.com/swobuforge/swobu/internal/adapters/outbound/ledger/sqlite"
	mediaadapter "github.com/swobuforge/swobu/internal/adapters/outbound/media"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/app/operator/authplane"
	chatgptlogin "github.com/swobuforge/swobu/internal/app/operator/chatgptlogin"
	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	operatorwire "github.com/swobuforge/swobu/internal/app/operator/controlplane/schema/operatorwire"
	"github.com/swobuforge/swobu/internal/app/operator/credentialfiles"
	"github.com/swobuforge/swobu/internal/app/operator/credentialmaterialization"
	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/app/operator/shares"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/delivery"
	executionevidenceDomain "github.com/swobuforge/swobu/internal/domain/executionevidence"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/export/ndjson"
	"github.com/swobuforge/swobu/internal/export/otlp"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/operatorweb"
	"github.com/swobuforge/swobu/internal/outputruntime"
	"github.com/swobuforge/swobu/internal/sharestate"
	"github.com/swobuforge/swobu/internal/sharetransport"
	"github.com/swobuforge/swobu/shareprotocol"
)

func buildDaemonServeMux(
	daemon *Daemon,
	addr string,
	runtime daemonProviderModelCatalogComposition,
	authCredentialWritePolicy credentialsadapter.CredentialWritePolicy,
) (*http.ServeMux, *chatgptlogin.LoginService, error) {
	executionEvidenceStore := executionevidencestore.NewStore(executionevidencestore.Config{})
	daemon.executionEvidenceStore = executionEvidenceStore
	ledgerWriter, ndjsonExporter, otlpExporter, err := composeM5Outputs(daemon, executionEvidenceStore)
	daemon.ledgerWriter = ledgerWriter
	daemon.ndjsonExporter = ndjsonExporter
	daemon.otlpExporter = otlpExporter
	if err != nil {
		// Optional M5 outputs are fail-open; status exposes their degraded state
		// while the gateway remains available for model/tool execution.
		daemon.logger.Warn("M5 output degraded", "error", err.Error())
	}
	exchangeIngress := exchange.NewIngress(
		daemon.configStore,
		runtime,
		exchange.RuntimePoliciesSpec{
			ImageFetcher:      mediaadapter.NewPublicImageFetcher(),
			ExecutionEvidence: executionEvidenceStore,
		},
	)
	shareTLS := &sharestate.TLSManager{Store: daemon.shareStore}
	shareHandler := httpapi.NewShareHandler(daemon.shareStore, daemon.configStore, exchangeIngress)
	relayAddress := strings.TrimSpace(os.Getenv("SWOBU_SHARE_RELAY_ADDR"))
	if relayAddress == "" {
		relayAddress = shareprotocol.RelayHostname + ":443"
	}
	shareRuntime, err := sharetransport.NewOwnerRuntime(relayAddress, nil, daemon.shareStore, shareTLS, shareHandler)
	if err != nil {
		return nil, nil, err
	}
	daemon.shareRuntime = shareRuntime
	mux := http.NewServeMux()
	mux.Handle("/c/", httpapi.NewHandlerWithExecutionObservation(exchangeIngress, func(out exchange.RequestOutput, result delivery.Result, timing *observation.Timing) {
		if daemon.telemetry == nil || out.ProductObservationInput == nil {
			return
		}
		projected := exchange.BuildProductObservation(out.ProductObservationInput, result)
		if projected == nil {
			return
		}
		if timing != nil {
			if duration, known := timing.DurationMillis(); known {
				projected.DurationMS = &duration
			}
			if firstByte, known := timing.TTFBMillis(); known {
				projected.TTFBMS = &firstByte
			}
		}
		daemon.telemetry.ObserveExecution(*projected)
	}))
	mux.Handle("/_swobu/status", httpapi.NewStatusHandler(func(context.Context) (httpapi.StatusDocument, error) {
		status, err := daemon.Status()
		if err != nil {
			return httpapi.StatusDocument{}, err
		}
		return httpapi.StatusDocument{
			State:                string(status.State),
			WorkspaceCount:       status.WorkspaceCount,
			ControlPlaneProtocol: controlplane.Protocol,
			SwobuVersion:         controlplane.SwobuVersion(),
			Ledger:               operatorHealth(status.Ledger),
			NDJSON:               ndjsonHealth(status.NDJSON),
			OTLP:                 otlpHealth(status.OTLP),
		}, nil
	}))
	mux.Handle("/_swobu/activity", httpapi.NewActivityHandler(func(_ *http.Request, scope executionevidence.ProjectionScope) (executionevidence.ActivityProjection, error) {
		return daemon.ActivityProjectionForScope(scope)
	}))
	mux.HandleFunc("/_swobu/telemetry-report", func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := daemon.inspectTelemetry(request.Context())
		if err != nil {
			http.Error(response, "telemetry report unavailable", http.StatusServiceUnavailable)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(body)
	})
	mux.Handle("/_swobu/down", httpapi.NewShutdownHandler(func(context.Context) error {
		go func() { _ = daemon.Close() }()
		return nil
	}))
	mux.Handle("/_swobu/target-probe", httpapi.NewTargetProbeHandler(runtime))
	persistCredential := func(providerSpec string, keyName string, secret string) (string, error) {
		return credentialsadapter.StoreMaterializedCredential(providerSpec, keyName, secret, authCredentialWritePolicy)
	}
	credentialMaterializer, err := credentialmaterialization.NewService(persistCredential, func(providerSpec, credentialRef string) error {
		return credentialsadapter.DeleteMaterializedCredential(providerSpec, credentialRef)
	})
	if err != nil {
		return nil, nil, err
	}
	if err := credentialMaterializer.SetPendingPath(daemon.configPath + ".credential-material.json"); err != nil {
		return nil, nil, fmt.Errorf("open pending credential metadata: %w", err)
	}
	daemon.materializer = &credentialMaterializer
	if err := credentialMaterializer.Reconcile(credentialReferences(daemon.configStore.Config()), time.Now()); err != nil {
		return nil, nil, fmt.Errorf("reconcile credential material registry: %w", err)
	}
	mux.Handle("/_swobu/credentials", httpapi.NewCredentialStoreHandler(credentialMaterializer.Materialize))
	mux.Handle("/_swobu/credential-files", httpapi.NewCredentialFilesHandler(
		func(_ context.Context, path string) (credentialfiles.Listing, error) {
			return credentialfiles.Browse(path)
		},
	))
	providerAuthoringHandler := httpapi.NewProviderCatalogHandler()
	mux.Handle("/_swobu/provider-authoring", providerAuthoringHandler)
	mux.Handle("/_swobu/provider-authoring/", providerAuthoringHandler)
	connectService, err := connectapp.NewService(clientconnect.NewService())
	if err != nil {
		return nil, nil, fmt.Errorf("Connect service: %w", err)
	}
	bindingCoordinator := &routebindings.Coordinator{}
	workspaceService, err := workspaces.NewService(daemon.configStore, daemon.shareStore, daemon.accessStore, daemon.shareRuntime, bindingCoordinator, &credentialMaterializer)
	if err != nil {
		return nil, nil, err
	}
	connectHandler := httpapi.NewConnectHandler(connectService, workspaceService)
	mux.Handle("/_swobu/connect/", connectHandler)
	workspaceControl := httpapi.NewWorkspaceControlHandler(workspaceService)
	mux.Handle("/_swobu/workspaces", workspaceControl)
	mux.Handle("/_swobu/workspaces/", workspaceControl)
	accessService, err := accessapp.NewService(daemon.accessStore, daemon.configStore, bindingCoordinator)
	if err != nil {
		return nil, nil, err
	}
	accessControl := httpapi.NewAccessControlHandler(accessService)
	mux.Handle("/_swobu/access", accessControl)
	mux.Handle("/_swobu/access/", accessControl)
	shareService, err := shares.NewService(daemon.configStore, daemon.shareStore, daemon.shareRuntime, bindingCoordinator)
	if err != nil {
		return nil, nil, err
	}
	mux.Handle("/_swobu/shares", httpapi.NewShareControlHandler(shareService))
	daemon.shareRuntime.StartIfActive()
	chatGPTLogin := chatgptlogin.NewService(newProviderHTTPClient(), chatgptlogin.ServiceConfig{
		PublicBaseURL: daemonPublicBaseURLFromAddr(addr),
		CredentialOut: chatgptlogin.CredentialWriterFunc(persistCredential),
	})
	authDriver, err := authplane.NewChatGPTAuthMethodDriver(chatGPTLogin)
	if err != nil {
		return nil, nil, fmt.Errorf("auth session driver: %w", err)
	}
	mux.HandleFunc("/_swobu/auth/chatgpt/callback", chatGPTLogin.HandleCallback)
	authStore := authplane.NewTargetCredentialStore(workspaceService)
	authManager, err := authplane.NewAuthSessionManager(authDriver, authStore)
	if err != nil {
		return nil, nil, fmt.Errorf("auth session manager: %w", err)
	}
	authSessionHandler := httpapi.NewAuthSessionHandler(authManager.Start, authManager.Poll, authManager.Cancel, authManager.Retry)
	mux.Handle("/_swobu/auth/sessions", authSessionHandler)
	mux.Handle("/_swobu/auth/sessions/", authSessionHandler)
	web, err := operatorweb.NewHandler()
	if err != nil {
		return nil, nil, fmt.Errorf("Web Operator assets: %w", err)
	}
	mux.Handle("/", web)

	protected := httpapi.NewLocalBrowserHandler(mux, daemon.accessStore, executionEvidenceStore)
	outer := http.NewServeMux()
	outer.Handle("/", protected)
	return outer, chatGPTLogin, nil
}

func operatorHealth(value *ledger.Health) *operatorwire.RuntimeSubsystemHealth {
	if value == nil {
		return nil
	}
	return &operatorwire.RuntimeSubsystemHealth{State: string(value.State), QueueDepth: value.QueueDepth, QueueCapacity: value.QueueCapacity, OfferedExchangesTotal: int(value.OfferedExchangesTotal), CommittedExchangesTotal: int(value.CommittedExchangesTotal), DroppedExchangesTotal: int(value.DroppedExchangesTotal), LastError: string(value.LastError)}
}
func ndjsonHealth(value *ndjson.Health) *operatorwire.RuntimeSubsystemHealth {
	if value == nil {
		return nil
	}
	return &operatorwire.RuntimeSubsystemHealth{State: string(value.State), QueueDepth: value.QueueDepth, QueueCapacity: value.QueueCapacity, Offered: int(value.Offered), Exported: int(value.Exported), Dropped: int(value.Dropped), LastError: string(value.LastError)}
}
func otlpHealth(value *otlp.Health) *operatorwire.RuntimeSubsystemHealth {
	if value == nil {
		return nil
	}
	return &operatorwire.RuntimeSubsystemHealth{State: string(value.State), QueueDepth: value.QueueDepth, QueueCapacity: value.QueueCapacity, Offered: int(value.Offered), Exported: int(value.Exported), Dropped: int(value.Dropped), LastError: string(value.LastError)}
}

func composeM5Outputs(daemon *Daemon, evidenceStore *executionevidencestore.Store) (*ledger.Writer, *ndjson.Exporter, *otlp.Exporter, error) {
	config := configuredM5()
	return composeConfiguredM5Outputs(daemon, evidenceStore, config)
}

// m5Config is the single bootstrap-owned snapshot of M5 environment policy.
// Constructors receive typed values and never rediscover process environment.
type m5Config struct {
	Backend      string
	LedgerPath   string
	PostgresDSN  string
	Retention    ledger.RetentionConfig
	NDJSONPath   string
	OTLPEnabled  bool
	OTLPEndpoint string
	LedgerErr    error
	OTLPErr      error
}

func configuredM5() m5Config {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("SWOBU_LEDGER")))
	if backend == "" {
		backend = "sqlite"
	}
	retention, retentionErr := configuredRetention(backend)
	c := m5Config{Backend: backend, LedgerPath: strings.TrimSpace(os.Getenv("SWOBU_LEDGER_PATH")), PostgresDSN: os.Getenv("SWOBU_LEDGER_POSTGRES_DSN"), Retention: retention, LedgerErr: retentionErr, NDJSONPath: strings.TrimSpace(os.Getenv("SWOBU_NDJSON_PATH"))}
	if c.Backend != "off" && c.Backend != "sqlite" && c.Backend != "postgres" {
		c.LedgerErr = fmt.Errorf("invalid SWOBU_LEDGER value %q", backend)
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SWOBU_OTEL")), "otlp") {
		c.OTLPEnabled = true
		c.OTLPEndpoint, c.OTLPErr = configuredOTLPEndpoint()
	} else if raw := strings.TrimSpace(os.Getenv("SWOBU_OTEL")); raw != "" && !strings.EqualFold(raw, "off") {
		c.OTLPErr = fmt.Errorf("invalid SWOBU_OTEL value %q", raw)
	}
	return c
}

func composeConfiguredM5Outputs(daemon *Daemon, evidenceStore *executionevidencestore.Store, config m5Config) (*ledger.Writer, *ndjson.Exporter, *otlp.Exporter, error) {
	var exporter *ndjson.Exporter
	var otlpExporter *otlp.Exporter
	var startupErr error
	if path := config.NDJSONPath; path != "" {
		var err error
		exporter, err = ndjson.New(ndjson.Config{Path: path})
		if err != nil {
			startupErr = err
			daemon.ndjsonStartupHealth = &ndjson.Health{State: ndjson.StateDegraded, LastError: outputruntime.ErrorExportFailed}
			exporter = nil
		} else {
		}
	}
	if config.LedgerErr != nil && config.Backend != "off" {
		startupErr = config.LedgerErr
		code := ledger.ErrorInvalidConfig
		daemon.ledgerStartupHealth = &ledger.Health{State: ledger.StateDegraded, LastError: code}
	}
	if config.OTLPEnabled {
		if config.OTLPErr != nil {
			if startupErr == nil {
				startupErr = config.OTLPErr
			}
			config.OTLPEnabled = false
			daemon.otlpStartupHealth = &otlp.Health{State: otlp.StateDegraded, LastError: outputruntime.ErrorExportFailed}
		}
	}
	if config.OTLPEnabled {
		var err error
		otlpExporter, err = otlp.New(context.Background(), otlp.Config{ServiceVersion: controlplane.SwobuVersion(), Endpoint: config.OTLPEndpoint})
		if err != nil {
			if startupErr == nil {
				startupErr = err
			}
			otlpExporter = nil
		}
	}
	backend := config.Backend
	if config.LedgerErr != nil {
		backend = "off"
	}
	if backend == "off" {
		if exporter != nil || otlpExporter != nil {
			evidenceStore.SetOnCompleted(func(completed executionevidenceDomain.CompletedExchange) {
				if exporter != nil {
					exporter.Offer(completed)
				}
				if otlpExporter != nil {
					otlpExporter.Offer(completed)
				}
			})
		}
		return nil, exporter, otlpExporter, startupErr
	}
	if backend == "" {
		backend = "sqlite"
	}
	observeExports := func(completed executionevidenceDomain.CompletedExchange) {
		if exporter != nil {
			exporter.Offer(completed)
		}
		if otlpExporter != nil {
			otlpExporter.Offer(completed)
		}
	}
	var store ledger.Store
	var err error
	switch backend {
	case "sqlite":
		path := config.LedgerPath
		if path == "" {
			path = filepath.Join(filepath.Dir(daemon.configPath), strings.TrimSuffix(filepath.Base(daemon.configPath), filepath.Ext(daemon.configPath))+".ledger.sqlite3")
		}
		store, err = ledgersqlite.Open(context.Background(), ledgersqlite.Config{Path: path, CreatedByVersion: controlplane.SwobuVersion(), Retention: config.Retention})
	case "postgres":
		store, err = ledgerpostgres.Open(context.Background(), ledgerpostgres.Config{DSN: config.PostgresDSN, CreatedByVersion: controlplane.SwobuVersion(), Retention: config.Retention})
	default:
		evidenceStore.SetOnCompleted(observeExports)
		if startupErr == nil {
			startupErr = fmt.Errorf("invalid SWOBU_LEDGER value %q", backend)
		}
		return nil, exporter, otlpExporter, startupErr
	}
	if err != nil {
		evidenceStore.SetOnCompleted(observeExports)
		var sqliteErr *ledgersqlite.OpenError
		var postgresErr *ledgerpostgres.OpenError
		code := ledger.ErrorOpenFailed
		if errors.As(err, &sqliteErr) {
			code = sqliteErr.Code
		}
		if errors.As(err, &postgresErr) {
			code = postgresErr.Code
		}
		daemon.ledgerStartupHealth = &ledger.Health{State: ledger.StateDegraded, LastError: code}
		if startupErr == nil {
			startupErr = err
		}
		return nil, exporter, otlpExporter, startupErr
	}
	writer, err := ledger.NewWriter(store, ledger.WriterConfig{})
	if err != nil {
		_ = store.Close()
		evidenceStore.SetOnCompleted(observeExports)
		if startupErr == nil {
			startupErr = err
		}
		return nil, exporter, otlpExporter, startupErr
	}
	// The M4 store remains the sole lifecycle assembler. Each enabled M5
	// sibling receives the immutable completion after the Store unlocks.
	evidenceStore.SetOnCompleted(func(completed executionevidenceDomain.CompletedExchange) {
		writer.Offer(completed)
		observeExports(completed)
	})
	return writer, exporter, otlpExporter, startupErr
}

func configuredOTLPEndpoint() (string, error) {
	protocol := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"))
	if protocol == "" {
		protocol = strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"))
	}
	protocol = strings.ToLower(protocol)
	if protocol != "" && protocol != "http/protobuf" {
		return "", errors.New("OTLP protocol must be http/protobuf")
	}
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"))
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
		if endpoint != "" {
			endpoint = strings.TrimRight(endpoint, "/") + "/v1/traces"
		}
	}
	if endpoint == "" {
		return "", errors.New("OTLP endpoint is required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("OTLP endpoint is invalid")
	}
	return endpoint, nil
}

func configuredRetention(backend string) (ledger.RetentionConfig, error) {
	var retention ledger.RetentionConfig
	if backend == "postgres" {
		retention = ledger.DefaultPostgresRetention()
	} else {
		retention = ledger.DefaultSQLiteRetention()
	}
	parse := func(name string, current int) (int, error) {
		raw, ok := os.LookupEnv(name)
		if !ok || strings.TrimSpace(raw) == "" {
			return current, nil
		}
		value, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || value <= 0 {
			return 0, fmt.Errorf("%s must be a positive integer", name)
		}
		return value, nil
	}
	var err error
	if retention.DetailDays, err = parse("SWOBU_LEDGER_DETAIL_RETENTION_DAYS", retention.DetailDays); err != nil {
		return ledger.RetentionConfig{}, err
	}
	if raw, ok := os.LookupEnv("SWOBU_LEDGER_MAX_BYTES"); ok && strings.TrimSpace(raw) != "" {
		if backend == "postgres" {
			return ledger.RetentionConfig{}, errors.New("SWOBU_LEDGER_MAX_BYTES is SQLite-only")
		}
		value, parseErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if parseErr != nil || value <= 0 {
			return ledger.RetentionConfig{}, errors.New("SWOBU_LEDGER_MAX_BYTES must be a positive integer")
		}
		retention.SQLiteMaxBytes = &value
	}
	if err := retention.Validate(); err != nil {
		return ledger.RetentionConfig{}, err
	}
	return retention, nil
}
