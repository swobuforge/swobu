package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/adapters/inbound/adminmcp"
	"github.com/swobuforge/swobu/internal/adapters/inbound/httpapi"
	credentialsadapter "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	executionevidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	mediaadapter "github.com/swobuforge/swobu/internal/adapters/outbound/media"
	"github.com/swobuforge/swobu/internal/admission"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/app/operator/authplane"
	chatgptlogin "github.com/swobuforge/swobu/internal/app/operator/chatgptlogin"
	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	connectionapp "github.com/swobuforge/swobu/internal/app/operator/connections"
	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	limitapp "github.com/swobuforge/swobu/internal/app/operator/limits"
	"github.com/swobuforge/swobu/internal/app/operator/managementaudit"
	publicationapp "github.com/swobuforge/swobu/internal/app/operator/publication"
	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/nodeauthority"
	"github.com/swobuforge/swobu/internal/operatorweb"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/relaystate"
	"github.com/swobuforge/swobu/internal/relaytransport"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/relayprotocol"
)

// buildDaemonServeMux wires the local control and operator endpoints around
// the separate public Workspace data plane. Activity reads project the same
// bounded execution-evidence store used by the request path.
func buildDaemonServeMux(
	daemon *Daemon,
	addr string,
	runtime daemonProviderModelCatalogComposition,
	authCredentialWritePolicy credentialsadapter.CredentialWritePolicy,
) (http.Handler, *chatgptlogin.LoginService, error) {
	limitAdmission := admission.New(func(slug routing.WorkspaceSlug) (string, routing.ServiceLimits, error) {
		workspace, err := daemon.configStore.GetWorkspace(context.Background(), slug)
		if err != nil {
			return "", routing.ServiceLimits{}, err
		}
		return workspace.ManagementIncarnation(), workspace.ServiceLimits(), nil
	})
	var estimator exchange.PriceEstimator
	if daemon.pricing != nil {
		estimator = daemon.pricing
	}
	exchangeIngress := exchange.NewIngress(
		daemon.configStore,
		runtime,
		exchange.RuntimePoliciesSpec{
			ModelConnection: daemon.connectionStore.Get,
			PublishRefreshedCredential: func(ctx context.Context, target provider.TargetSnapshot, material string) error {
				if daemon.configStore.ReadOnly() {
					return configstore.ErrReadOnly
				}
				return publishRefreshedConnection(ctx, daemon.connectionStore, target, material, authCredentialWritePolicy)
			},
			ImageFetcher:      mediaadapter.NewPublicImageFetcher(),
			ExecutionEvidence: daemon.executionEvidenceStore,
			Accounting:        daemon.accountingStore,
			PriceEstimator:    estimator,
			WorkspaceToolCatalog: func(ctx context.Context, workspace routing.Workspace) ([]mcp.CatalogTool, error) {
				if _, err := httpapi.WorkspaceServiceAuthorityFromContext(ctx, workspace.Slug(), routing.ServiceMCP); err != nil {
					return nil, canonical.BadRequest("Workspace MCP tool access is not authorized")
				}
				return mcp.WorkspaceCatalog(workspace, daemon.workspaceMCPBindings(workspace))
			},
			WorkspaceToolCall: func(ctx context.Context, principal accessidentity.ID, frozenWorkspace routing.Workspace, tool mcp.CatalogTool, call canonical.ToolCallItem) (canonical.CanonicalItem, error) {
				currentPrincipal, err := httpapi.WorkspaceServiceAuthorityFromContext(ctx, frozenWorkspace.Slug(), routing.ServiceMCP)
				if err != nil || currentPrincipal != principal {
					return canonical.CanonicalItem{}, canonical.BadRequest("Workspace MCP tool authority is no longer valid")
				}
				currentWorkspace, err := daemon.configStore.GetWorkspace(ctx, frozenWorkspace.Slug())
				if err != nil || frozenWorkspace.ConfigurationRevision() == "" || currentWorkspace.ConfigurationRevision() != frozenWorkspace.ConfigurationRevision() {
					return canonical.CanonicalItem{}, canonical.BadRequest("Workspace configuration changed; select the catalog again")
				}
				// Models composition uses this callback, independently of the direct
				// MCP handler. Charge MCP at its effect boundary before dispatch.
				if retry, err := httpapi.AdmitComposedServiceOperation(ctx, frozenWorkspace.Slug(), routing.ServiceMCP); err != nil {
					if errors.Is(err, accessstate.ErrRequestRateLimited) {
						return canonical.CanonicalItem{}, canonical.RequestRateLimited("Principal MCP request limit exceeded", retry)
					}
					return canonical.CanonicalItem{}, canonical.BadRequest("Workspace MCP tool authority is no longer valid")
				}
				release, retry, err := limitAdmission.Admit(frozenWorkspace.Slug(), routing.ServiceMCP)
				if err != nil {
					if errors.Is(err, admission.ErrExhausted) {
						return canonical.CanonicalItem{}, canonical.RequestRateLimited("Workspace MCP operational limit exceeded", retry)
					}
					return canonical.CanonicalItem{}, canonical.InternalError("Workspace MCP operational admission unavailable")
				}
				defer release()
				return mcp.CallWorkspaceToolResult(ctx, currentWorkspace, daemon.workspaceMCPBindings(currentWorkspace), tool, call)
			},
		},
	)
	relayTLS := &relaystate.TLSManager{Store: daemon.relayStore}
	web, err := operatorweb.NewHandler()
	if err != nil {
		return nil, nil, fmt.Errorf("Web Operator assets: %w", err)
	}
	mux := http.NewServeMux()
	connectionCommands, err := connectionapp.NewCommands(daemon.connectionStore, daemon.connectionLifecycle)
	if err != nil {
		return nil, nil, err
	}
	connectionControl := httpapi.NewConnectionControlHandler(daemon.connectionQueries, connectionCommands, func(ctx context.Context, id connectionidentity.ID, slot, secret string, fresh bool) (credentialref.Ref, error) {
		if err := ctx.Err(); err != nil {
			return credentialref.Ref{}, err
		}
		raw, err := credentialsadapter.MaterializeConnectionCredential(id.String(), slot, secret, authCredentialWritePolicy, fresh)
		if errors.Is(err, credentialsadapter.ErrConnectionMaterialConflict) {
			return credentialref.Ref{}, connectionapp.ErrMaterialConflict
		}
		return credentialref.Parse(raw), err
	}, checkDaemonConnection)
	// GET /api/v1/node projects only counts and the whole-file revision.
	// It never exposes secret references, stored credential digests, or YAML.
	nodeOverview := func(ctx context.Context) (nodeauthority.Overview, error) {
		if err := ctx.Err(); err != nil {
			return nodeauthority.Overview{}, err
		}
		if daemon.nodeFile == nil {
			return nodeauthority.Overview{}, errors.New("Node file unavailable")
		}
		raw, revision, err := daemon.nodeFile.Snapshot()
		if err != nil {
			return nodeauthority.Overview{}, err
		}
		summary, err := nodeauthority.Validate(raw)
		if err != nil {
			return nodeauthority.Overview{}, err
		}
		mode := "managed"
		if daemon.configStore.ReadOnly() {
			mode = "externally_managed"
		}
		return nodeauthority.Overview{
			Revision: revision, ConfigurationMode: mode,
			Workspaces: summary.Workspaces, Connections: summary.Connections,
			Principals: summary.Principals, Publications: summary.Publications,
			BudgetPolicies: summary.BudgetPolicies,
		}, nil
	}
	mux.Handle("GET /api/v1/node", httpapi.NewNodeOverviewHandler(nodeOverview))
	mux.Handle("/api/v1/connections", connectionControl)
	mux.Handle("/api/v1/connections/{id}/mcp-catalog", httpapi.NewConnectionMCPCatalogHandler(func(ctx context.Context, id connectionidentity.ID) ([]mcp.CatalogTool, error) {
		return mcp.ConnectionCatalog(id.String(), daemon.currentMCPBindings(id.String()))
	}))
	mux.Handle("/api/v1/connections/", connectionControl)
	bindingCoordinator := &routebindings.Coordinator{}
	accessService, err := accessapp.NewService(daemon.accessStore, daemon.configStore, bindingCoordinator)
	if err != nil {
		return nil, nil, err
	}
	connectService, err := connectapp.NewService(clientconnect.NewService(), accessService)
	if err != nil {
		return nil, nil, err
	}
	mux.Handle("/api/v1/clients", httpapi.NewClientsHandler(connectService, "http://"+addr, func(request *http.Request, slug routing.WorkspaceSlug) bool {
		_, err := daemon.configStore.GetWorkspace(request.Context(), slug)
		return err == nil
	}))
	attemptDirectory := filepath.Join(daemon.runtimeDir, "connect-attempts")
	if err := os.MkdirAll(attemptDirectory, 0700); err != nil {
		return nil, nil, err
	}
	connectAttempts, err := connectapp.OpenAttempts(attemptDirectory)
	if err != nil {
		return nil, nil, err
	}
	mux.Handle("/api/v1/client-configurations", httpapi.NewClientConfigurationHandler(connectService, connectAttempts, "http://"+addr, func(request *http.Request, slug routing.WorkspaceSlug, modelID string) bool {
		workspace, err := daemon.configStore.GetWorkspace(request.Context(), slug)
		if err != nil {
			return false
		}
		_, err = workspace.ResolveRoute(modelID)
		return err == nil
	}))
	activityHandler := httpapi.NewActivityHandler(func(request *http.Request, scope executionevidence.ProjectionScope, page ledger.ActivityPageQuery) (executionevidence.ActivityProjection, error) {
		if page.Limit != 0 {
			return daemon.activityPage(request.Context(), scope, page)
		}
		return daemon.activity(request.Context(), scope)
	})
	mux.Handle("/api/v1/activity", activityHandler)
	mux.Handle("GET /api/v1/activity/export", httpapi.NewActivityExportHandler(func(request *http.Request, scope executionevidence.ProjectionScope, page ledger.ActivityPageQuery) (executionevidence.ActivityProjection, error) {
		return daemon.activityPage(request.Context(), scope, page)
	}))
	mux.Handle("/api/v1/activity/stream", httpapi.NewActivityStreamHandler(func(request *http.Request, scope executionevidence.ProjectionScope, _ ledger.ActivityPageQuery) (executionevidence.ActivityProjection, error) {
		return daemon.activity(request.Context(), scope)
	}))
	mux.Handle("/api/v1/activity/operations/", httpapi.ActivityDetailHandler{Read: func(request *http.Request, identity string) (executionevidence.OperationDetail, bool, error) {
		return daemon.activityDetail(request.Context(), identity)
	}})
	// Canonical Workspace data-plane projection is separately constructed from
	// Node Control; no control or Web handlers are reachable beneath this root.
	workspaceData := httpapi.NewWorkspaceDataHandler(exchangeIngress)
	workspaceMCP := mcp.NewWorkspaceMCPHandler(mcp.WorkspaceServerConfig{
		Authorize: httpapi.AuthorizeWorkspaceRequest,
		Catalog: func(_ context.Context, slug string) ([]mcp.CatalogTool, error) {
			workspaceSlug, err := routing.ParseWorkspaceSlug(slug)
			if err != nil {
				return nil, err
			}
			workspace, err := daemon.configStore.GetWorkspace(context.Background(), workspaceSlug)
			if err != nil {
				return nil, err
			}
			if !workspace.HasService(routing.ServiceMCP) {
				return nil, routing.ErrServiceDisabled
			}
			return mcp.WorkspaceCatalog(workspace, daemon.workspaceMCPBindings(workspace))
		},
		Call: func(ctx context.Context, slug string, tool mcp.CatalogTool, args json.RawMessage) (*sdk.CallToolResult, error) {
			workspaceSlug, err := routing.ParseWorkspaceSlug(slug)
			if err != nil {
				return nil, err
			}
			workspace, err := daemon.configStore.GetWorkspace(ctx, workspaceSlug)
			if err != nil {
				return nil, err
			}
			if !workspace.HasService(routing.ServiceMCP) {
				return nil, routing.ErrServiceDisabled
			}
			principal, err := httpapi.WorkspaceAuthorityFromContext(ctx, workspaceSlug)
			if err != nil {
				return nil, err
			}
			currentBindings := daemon.workspaceMCPBindings(workspace)
			bindings := make(map[string]mcp.SourceBinding, len(currentBindings))
			for identity, binding := range currentBindings {
				bindings[identity] = binding
			}
			binding, ok := bindings[tool.SourceID]
			if !ok || binding.Call == nil {
				return nil, fmt.Errorf("MCP source is unavailable")
			}
			call := binding.Call
			binding.Call = func(callCtx context.Context, name string, arguments json.RawMessage) (*sdk.CallToolResult, error) {
				currentPrincipal, err := httpapi.WorkspaceAuthorityFromContext(callCtx, workspaceSlug)
				if err != nil || currentPrincipal != principal {
					return nil, fmt.Errorf("MCP authority changed before dispatch")
				}
				if _, err := httpapi.AdmitComposedServiceOperation(callCtx, workspaceSlug, routing.ServiceMCP); err != nil {
					return nil, fmt.Errorf("MCP tool request denied: %w", err)
				}
				// MCP discovery and initialization are free; one actual tools/call
				// consumes one Workspace budget slot (including built-in sources).
				release, retry, err := limitAdmission.Admit(workspaceSlug, routing.ServiceMCP)
				if err != nil {
					if errors.Is(err, admission.ErrExhausted) {
						return nil, canonical.RequestRateLimited("Workspace MCP operational limit exceeded", retry)
					}
					return nil, canonical.InternalError("Workspace MCP operational admission unavailable")
				}
				defer release()
				identity, err := daemon.executionEvidenceStore.BeginMCP(principal, workspaceSlug, tool.SourceID, name)
				if err != nil {
					return nil, err
				}
				if tool.SourceID == "builtin:"+routing.AdminMCPSource {
					// Capture immutable intent and the admitted credential closure,
					// never an HTTP local-host identity or bearer token. Config reads
					// use the atomic snapshot and do not reenter its write mutex.
					check := func(effectCtx context.Context) error {
						identity, err := httpapi.WorkspaceServiceAuthorityFromContext(effectCtx, workspaceSlug, routing.ServiceMCP)
						if err != nil || identity != principal {
							return fmt.Errorf("Admin authority changed")
						}
						current, err := daemon.configStore.GetWorkspace(effectCtx, workspaceSlug)
						if err != nil || !current.HasService(routing.ServiceMCP) || current.ConfigurationRevision() != workspace.ConfigurationRevision() {
							return fmt.Errorf("Admin Workspace configuration changed; select the catalog again")
						}
						catalog, err := mcp.WorkspaceCatalog(current, bindings)
						if err != nil {
							return fmt.Errorf("Admin source unavailable")
						}
						for _, entry := range catalog {
							if entry.Name == tool.Name && entry.SourceID == tool.SourceID {
								return nil
							}
						}
						return fmt.Errorf("Admin tool is no longer mounted")
					}
					callCtx = adminmcp.WithAuthority(callCtx, principal, check)
				}
				result, callErr := call(callCtx, name, arguments)
				if result == nil && callErr == nil {
					callErr = fmt.Errorf("MCP source returned no result")
				}
				outcome, occurrence := execution.Succeeded, execution.Occurred
				if callErr != nil || result == nil {
					outcome, occurrence = execution.Failed, execution.MayHaveOccurred
					if errors.Is(callErr, context.Canceled) {
						outcome = execution.Cancelled
					}
				} else if result.IsError {
					outcome = execution.Failed
				}
				if settleErr := daemon.executionEvidenceStore.SettleMCP(identity, outcome, occurrence); settleErr != nil {
					return nil, settleErr
				}
				return result, callErr
			}
			bindings[tool.SourceID] = binding
			return mcp.CallWorkspaceTool(ctx, workspace, bindings, tool, args)
		},
	})
	publicWorkspaceData := httpapi.NewWorkspaceDataHandler(exchangeIngress)
	workspaceSearch := httpapi.NewWorkspaceSearchHandler(daemon.searchService, httpapi.AuthorizeWorkspaceRequest)
	publicWorkspace := httpapi.NewPublicWorkspaceHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/mcp") {
			workspaceMCP.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/workspaces/"+r.PathValue("workspace")+"/search" {
			workspaceSearch.ServeHTTP(w, r)
			return
		}
		publicWorkspaceData.ServeHTTP(w, r)
	}), daemon.accessStore, daemon.publicationStore, limitAdmission.Admit)
	relayAddress := strings.TrimSpace(os.Getenv("SWOBU_RELAY_ADDR"))
	if relayAddress == "" {
		relayAddress = relayprotocol.RelayHostname + ":443"
	}
	publicHandler := publicWorkspace
	relayRuntime, err := relaytransport.NewOwnerRuntime(relayAddress, nil, daemon.relayStore, relayTLS, publicHandler)
	if err != nil {
		return nil, nil, err
	}
	relayRuntime.HasPublicWorkspace = daemon.publicationStore.HasPublicWorkspace
	daemon.relayRuntime = relayRuntime
	daemon.publicHandler = publicHandler
	mux.Handle("/workspaces/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SPA documents and client data share the Workspace path root, but
		// must never share their authority or handler implementation.
		if operatorweb.IsSPARoute(r.URL.EscapedPath()) {
			web.ServeHTTP(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/mcp") {
			workspaceMCP.ServeHTTP(w, r)
			return
		}
		workspaceData.ServeHTTP(w, r)
	}))
	mux.Handle("/workspaces/{workspace}/search", workspaceSearch)
	statusHandler := httpapi.NewStatusHandler(func(context.Context) (httpapi.StatusDocument, error) {
		status, err := daemon.Status()
		if err != nil {
			return httpapi.StatusDocument{}, err
		}
		return httpapi.StatusDocument{
			State:                string(status.State),
			WorkspaceCount:       status.WorkspaceCount,
			ControlPlaneProtocol: controlplane.Protocol,
			SwobuVersion:         controlplane.SwobuVersion(),
		}, nil
	})
	mux.Handle("/api/v1/status", statusHandler)
	mux.HandleFunc("GET /api/v1/telemetry/report", func(response http.ResponseWriter, request *http.Request) {
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
		response.Header().Set("Cache-Control", "no-store")
		_, _ = response.Write(body)
	})
	mux.Handle("POST /api/v1/daemon/shutdown", httpapi.NewShutdownHandler(func(context.Context) error {
		go func() { _ = daemon.Close() }()
		return nil
	}))
	persistCredential := func(scope credentialref.Scope, keyName string, secret string) (string, error) {
		if scope.Kind() != credentialref.ScopeProvider || !scope.Valid() {
			return "", fmt.Errorf("credential scope is invalid")
		}
		return credentialsadapter.StoreMaterializedCredential(scope.Identifier(), keyName, secret, authCredentialWritePolicy)
	}
	workspaceService, err := workspaces.NewService(daemon.configStore, workspaces.Dependencies{
		Connections:  daemon.connectionStore,
		Access:       daemon.accessStore,
		Bindings:     bindingCoordinator,
		Publications: daemon.publicationStore,
		Accounting:   daemon.accountingStore,
	})
	if err != nil {
		return nil, nil, err
	}
	// Node Control is local and browser-protected. The public data-plane
	// handler is separate and never receives this subtree.
	nodeControl := httpapi.NewNodeControlAPIHandler(workspaceService)
	mux.Handle("/api/v1/workspaces", nodeControl)
	mux.Handle("/api/v1/workspaces/", nodeControl)
	mux.Handle("/api/v1/search-evidence", httpapi.NewSearchEvidenceHandler(daemon.searchEvidence))
	workspaceCatalog := func(ctx context.Context, slug string) ([]mcp.CatalogTool, error) {
		workspaceSlug, err := routing.ParseWorkspaceSlug(slug)
		if err != nil {
			return nil, err
		}
		workspace, err := daemon.configStore.GetWorkspace(ctx, workspaceSlug)
		if err != nil {
			return nil, err
		}
		if !workspace.HasService(routing.ServiceMCP) {
			return nil, routing.ErrServiceDisabled
		}
		return mcp.WorkspaceCatalog(workspace, daemon.workspaceMCPBindings(workspace))
	}
	mux.Handle("/api/v1/workspaces/{workspace}/tools", httpapi.NewWorkspaceToolsHandler(workspaceCatalog))
	accessControl := httpapi.NewAccessControlHandler(accessService)
	auditService, err := managementaudit.New(daemon.accessStore, daemon.accountingStore, connectAttempts, daemon.configStore, daemon.connectionStore)
	if err != nil {
		return nil, nil, err
	}
	mux.Handle("/api/v1/audit/events", httpapi.NewAuditHandler(auditService))
	mux.Handle("/api/v1/principals", accessControl)
	mux.Handle("/api/v1/principals/", accessControl)
	mux.Handle("/api/v1/workspaces/{workspace}/permissions/{principal}", accessControl)
	workspaceExists := func(slug string) bool {
		parsed, err := routing.ParseWorkspaceSlug(slug)
		if err != nil {
			return false
		}
		_, err = daemon.configStore.GetWorkspace(context.Background(), parsed)
		return err == nil
	}
	publicationControl := httpapi.PublicationHandler{
		Store:  daemon.publicationStore,
		Exists: func(slug routing.WorkspaceSlug) bool { return workspaceExists(slug.String()) },
		Ready: func(ctx context.Context) (func(), error) {
			lease, err := daemon.relayRuntime.EnsurePublicWorkspaceLease(ctx)
			if err != nil {
				return nil, err
			}
			return lease.Release, nil
		},
		StopIfInactive: daemon.relayRuntime.StopIfInactive,
		RelayURL: func() string {
			id, err := daemon.relayStore.EndpointID()
			if err != nil {
				return ""
			}
			return "https://" + relaystate.Hostname(id)
		},
	}
	mux.Handle("GET /api/v1/remote-addresses", publicationControl)
	mux.Handle("GET /api/v1/workspaces/{workspace}/publications", publicationControl)
	mux.Handle("/api/v1/workspaces/{workspace}/publications/relay", publicationControl)
	limitsHandler := httpapi.NewLimitsControlHandler(daemon.accountingStore, workspaceExists).WithApprovals(daemon.adminApprovals)
	mux.Handle("/api/v1/workspaces/{workspace}/limits", limitsHandler)
	mux.Handle("GET /api/v1/workspaces/{workspace}/limits/changes", limitsHandler)
	mux.Handle("GET /api/v1/workspaces/{workspace}/limits/status", limitsHandler)
	mux.Handle("/api/v1/workspaces/{workspace}/limits/preview", limitsHandler)
	mux.Handle("/api/v1/workspaces/{workspace}/limits/apply", limitsHandler)
	adminTools := adminmcp.WorkspaceTools(workspaceService)
	adminTools = append(adminTools, adminmcp.WorkspaceCatalogTools(workspaceCatalog)...)
	adminTools = append(adminTools, adminmcp.NodeTools(nodeOverview, func() (string, error) { status, err := daemon.Status(); return string(status.State), err })...)
	adminTools = append(adminTools, adminmcp.MCPCatalogTools(func(ctx context.Context, id connectionidentity.ID) ([]mcp.CatalogTool, error) {
		return mcp.ConnectionCatalog(id.String(), daemon.currentMCPBindings(id.String()))
	})...)
	adminTools = append(adminTools, adminmcp.ConnectionAuthoringTools(connectionCommands)...)
	adminTools = append(adminTools, adminmcp.ConnectTools(connectService, "http://"+addr, daemon.configStore.GetWorkspace)...)
	adminTools = append(adminTools, adminmcp.ConnectionTools(daemon.connectionQueries, connectionCommands, checkDaemonConnection)...)
	adminTools = append(adminTools, adminmcp.AccessQueries(accessService)...)
	adminTools = append(adminTools, adminmcp.SecretHandoffs(accessService, daemon.connectionQueries)...)
	adminTools = append(adminTools, adminmcp.LimitTools(limitapp.New(daemon.accountingStore, daemon.adminApprovals, workspaceExists))...)
	adminTools = append(adminTools, adminmcp.PublicationTools(publicationapp.Service{Store: publicationControl.Store, Exists: publicationControl.Exists, Ready: publicationControl.Ready, StopIfInactive: publicationControl.StopIfInactive, RelayURL: publicationControl.RelayURL})...)
	adminTools = append(adminTools, adminmcp.ActivityTools(func(ctx context.Context, scope executionevidence.ProjectionScope, page ledger.ActivityPageQuery) (executionevidence.ActivityProjection, error) {
		if page.Limit != 0 {
			return daemon.activityPage(ctx, scope, page)
		}
		return daemon.activity(ctx, scope)
	}, daemon.activityDetail)...)
	daemon.adminMCPBinding, err = mcp.NewAdminSourceBinding(adminTools)
	if err != nil {
		return nil, nil, err
	}
	daemon.relayRuntime.StartIfActive()
	chatGPTLogin := chatgptlogin.NewService(newProviderHTTPClient(), chatgptlogin.ServiceConfig{
		PublicBaseURL: daemonPublicBaseURLFromAddr(addr),
		CredentialOut: chatgptlogin.CredentialWriterFunc(func(providerSpec, keyName, secret string) (string, error) {
			scope, err := credentialref.NewProviderScope(providerSpec)
			if err != nil {
				return "", err
			}
			return persistCredential(scope, keyName, secret)
		}),
	})
	authDriver, err := authplane.NewChatGPTAuthMethodDriver(chatGPTLogin)
	if err != nil {
		return nil, nil, fmt.Errorf("auth session driver: %w", err)
	}
	authManager, err := authplane.NewAuthSessionManager(authDriver)
	if err != nil {
		return nil, nil, fmt.Errorf("auth session manager: %w", err)
	}
	authAttemptDirectory := filepath.Join(daemon.runtimeDir, "auth-start-attempts")
	if err := os.MkdirAll(authAttemptDirectory, 0700); err != nil {
		return nil, nil, err
	}
	authAttempts, err := authplane.OpenStartAttempts(authAttemptDirectory)
	if err != nil {
		return nil, nil, err
	}
	authSessionHandler := httpapi.NewAuthSessionHandler(authManager.Poll, authManager.Cancel).WithStartAttempts(authManager, authAttempts)
	mux.Handle("/api/v1/auth-sessions", authSessionHandler)
	mux.Handle("/api/v1/auth-sessions/", authSessionHandler)
	mux.Handle("/", web)
	// The collection document is an exact UI route. Without this registration,
	// ServeMux redirects it into the Workspace data subtree before SPA fallback.
	mux.Handle("/workspaces", web)

	// Admission must be the outermost handler: a wrapping ServeMux would clean
	// ambiguous paths and redirect them before credential/path admission runs.
	var handler http.Handler = mux
	if daemon.configStore.ReadOnly() {
		handler = readOnlyNodeControlGate(handler)
	}
	return httpapi.NewLocalAdmissionHandler(httpapi.NewBrowserSessions().Protect(handler), daemon.accessStore, limitAdmission.Admit), chatGPTLogin, nil
}
