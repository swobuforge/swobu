package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// TargetSetupQueries reads provider options and model catalogs for the
// add-target workflow.
//
// Adapter ownership: LiveOperatorAdapter must project from profile catalog
// and operator client model-catalog probe.
type TargetSetupQueries interface {
	ListTargetProviders(ctx context.Context) ([]readmodel.ProviderOptionReadModel, error)
	ResolveProviderSetup(ctx context.Context, req ResolveProviderSetupRequest) (readmodel.ProviderSetupReadModel, error)
	ProbeProviderModels(ctx context.Context, req ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error)
}

// ResolveProviderSetupRequest names the provider and any current setup inputs
// so the adapter can project the provider-setup state honestly.
type ResolveProviderSetupRequest struct {
	ProviderSpec  string
	BaseURL       string
	CredentialRef string
}

// ProbeProviderModelsRequest names the provider and any resolved setup facts
// so the adapter can probe the model catalog.
type ProbeProviderModelsRequest struct {
	ProviderSpec     string
	BaseURL          string
	AuthHeader       string
	CredentialRef    string
	ProviderProtocol string
}

// TargetAuthCommands manages interactive auth session lifecycle for providers
// that require browser or device login.
//
// Adapter ownership: LiveOperatorAdapter wraps the operator client's auth
// session endpoints.
type TargetAuthCommands interface {
	StartAuthSession(ctx context.Context, req StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error)
	PollAuthSession(ctx context.Context, sessionID string) (readmodel.AuthSessionReadModel, error)
	CancelAuthSession(ctx context.Context, sessionID string) error
	RetryAuthSession(ctx context.Context, sessionID string) (readmodel.AuthSessionReadModel, error)
}

// StartAuthSessionRequest names the provider and auth mode for an interactive
// session. EndpointRef may be a transient subject locator while the target is
// still being created.
type StartAuthSessionRequest struct {
	ProviderSpec string
	EndpointRef  string
	AuthMode     string
}

// Compile assertions — use the adapter package instead (ports imports adapters
// would create cycle).
