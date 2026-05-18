package bootstrap

import (
	"context"
	"fmt"
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/inbound/httpapi"
	credentialsadapter "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	evidencestore "github.com/swobuforge/swobu/internal/adapters/outbound/evidence"
	"github.com/swobuforge/swobu/internal/app/operator/authplane"
	chatgptlogin "github.com/swobuforge/swobu/internal/app/operator/chatgptlogin"
	"github.com/swobuforge/swobu/internal/app/operator/controlplane"
	operatorendpoints "github.com/swobuforge/swobu/internal/app/operator/endpoints"
	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/ports"
)

func buildDaemonServeMux(
	daemon *Daemon,
	bindAddr string,
	providers ports.ProviderExecutor,
	modelCatalog ports.ProviderModelCatalog,
	evidence ports.RequestEvidenceSink,
	continuity ports.ResponseContinuityStore,
	authCredentialWritePolicy credentialsadapter.CredentialWritePolicy,
) (*http.ServeMux, *chatgptlogin.LoginService, error) {
	orchestrator := requestpath.NewRequestHandler(daemon.endpoints, providers, evidence, continuity)
	mux := http.NewServeMux()
	mux.Handle("/c/", httpapi.NewHandler(orchestrator))
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
	mux.Handle("/_swobu/down", httpapi.NewShutdownHandler(func(context.Context) error {
		go func() { _ = daemon.Close(context.Background()) }()
		return nil
	}))
	mux.Handle("/_swobu/model-catalog/probe", httpapi.NewModelCatalogProbeHandler(modelCatalog))
	endpointIntent := operatorendpoints.NewOperatorEndpointStore(daemon.endpoints)
	chatGPTLogin := chatgptlogin.NewService(newProviderHTTPClient(), chatgptlogin.ServiceConfig{
		PublicBaseURL: daemonPublicBaseURLFromBindAddr(bindAddr),
		CredentialOut: chatgptlogin.CredentialWriterFunc(func(providerSpec string, keyName string, secret string) (string, error) {
			return credentialsadapter.StoreMaterializedCredential(providerSpec, keyName, secret, authCredentialWritePolicy)
		}),
	})
	authDriver, err := authplane.NewChatGPTAuthMethodDriver(chatGPTLogin)
	if err != nil {
		return nil, nil, fmt.Errorf("auth session driver: %w", err)
	}
	authStore := authplane.NewEndpointCredentialStore(endpointIntent)
	authManager, err := authplane.NewAuthSessionManager(authDriver, authStore)
	if err != nil {
		return nil, nil, fmt.Errorf("auth session manager: %w", err)
	}
	authSessionHandler := httpapi.NewAuthSessionHandler(
		func(ctx context.Context, in authplane.StartInput) (authplane.StartOutput, error) {
			return authManager.Start(ctx, in)
		},
		func(ctx context.Context, sessionID string) (authplane.SessionOutput, error) {
			return authManager.Poll(ctx, sessionID)
		},
		func(ctx context.Context, sessionID string) error { return authManager.Cancel(ctx, sessionID) },
		func(ctx context.Context, sessionID string) (authplane.StartOutput, error) {
			return authManager.Retry(ctx, sessionID)
		},
	)
	mux.Handle("/_swobu/auth/sessions", authSessionHandler)
	mux.Handle("/_swobu/auth/sessions/", authSessionHandler)
	mux.HandleFunc("/_swobu/auth/chatgpt/callback", chatGPTLogin.HandleCallback)
	controlHandler := httpapi.NewEndpointControlHandler(
		func(ctx context.Context) ([]endpointintent.Endpoint, error) { return endpointIntent.List(ctx) },
		func(ctx context.Context, name string) (endpointintent.Endpoint, error) {
			return endpointIntent.Get(ctx, name)
		},
		func(ctx context.Context, endpoint endpointintent.Endpoint) (endpointintent.Endpoint, error) {
			return endpointIntent.Put(ctx, endpoint)
		},
		func(ctx context.Context, name string) error { return endpointIntent.Delete(ctx, name) },
	)
	mux.Handle("/_swobu/endpoints", controlHandler)
	mux.Handle("/_swobu/endpoints/", controlHandler)
	return mux, chatGPTLogin, nil
}
