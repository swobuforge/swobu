package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/inbound/httpapi"
	credentialsadapter "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	mediaadapter "github.com/swobuforge/swobu/internal/adapters/outbound/media"
	trafficevidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/trafficevidence"
	"github.com/swobuforge/swobu/internal/app/operator/authplane"
	chatgptlogin "github.com/swobuforge/swobu/internal/app/operator/chatgptlogin"
	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	"github.com/swobuforge/swobu/internal/app/operator/shares"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/sharestate"
	"github.com/swobuforge/swobu/internal/sharetransport"
	"github.com/swobuforge/swobu/shareprotocol"
)

func buildDaemonServeMux(
	daemon *Daemon,
	addr string,
	runtime daemonProviderModelCatalogComposition,
	trafficEventSink observation.TrafficEventSink,
	authCredentialWritePolicy credentialsadapter.CredentialWritePolicy,
) (*http.ServeMux, *chatgptlogin.LoginService, error) {
	exchangeIngress := exchange.NewIngress(
		daemon.configStore,
		runtime,
		exchange.RuntimePoliciesSpec{
			ImageFetcher:    mediaadapter.NewPublicImageFetcher(),
			TrafficEvidence: trafficEventSink,
		},
	)
	shareTLS := &sharestate.TLSManager{Store: daemon.shareStore}
	shareHandler := httpapi.NewShareHandler(daemon.shareStore, daemon.configStore, exchangeIngress, trafficEventSink)
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
	mux.Handle("/c/", httpapi.NewHandler(exchangeIngress, trafficEventSink))
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
		}, nil
	}))
	mux.Handle("/_swobu/status-projection", httpapi.NewStatusProjectionHandler(func(_ context.Context, scope trafficevidencestore.ProjectionScope) (trafficevidencestore.StatusProjection, error) {
		return daemon.StatusProjectionForScope(scope)
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
	mux.Handle("/_swobu/credentials", httpapi.NewCredentialStoreHandler(
		func(_ context.Context, providerSpec string, keyName string, secret string) (string, error) {
			return persistCredential(providerSpec, keyName, secret)
		},
	))
	workspaceService, err := workspaces.NewService(daemon.configStore)
	if err != nil {
		return nil, nil, err
	}
	workspaceControl := httpapi.NewWorkspaceControlHandler(workspaceService)
	mux.Handle("/_swobu/workspaces", workspaceControl)
	mux.Handle("/_swobu/workspaces/", workspaceControl)
	shareService, err := shares.NewService(daemon.configStore, daemon.shareStore, daemon.shareRuntime)
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

	return mux, chatGPTLogin, nil
}
