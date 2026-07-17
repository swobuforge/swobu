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
	ProbeProviderModels(ctx context.Context, req ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error)
}

// ProbeProviderModelsRequest names the provider and any resolved setup facts
// so the adapter can probe the model catalog.
type ProbeProviderModelsRequest struct {
	ProviderSpec     string
	BaseURL          string
	AuthHeader       string
	CredentialRef    string
	AuthMode         string
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

// TargetCredentialCommands persists transient credential material entered from
// target setup and returns a durable credential reference for target drafts.
type TargetCredentialCommands interface {
	StorePastedCredential(ctx context.Context, req StorePastedCredentialRequest) (StorePastedCredentialResult, error)
}

// StorePastedCredentialRequest carries pasted secret material across the
// cockpit adapter boundary. UI components must keep only the returned ref.
type StorePastedCredentialRequest struct {
	ProviderSpec string
	// Name is a generated storage slot, not user-facing input.
	// Callers should include enough semantic context for diagnostics and a
	// unique suffix to avoid overwriting another secret.
	Name   string
	Secret string
}

// StorePastedCredentialResult returns the persisted credential reference.
type StorePastedCredentialResult struct {
	CredentialRef string
}

// Compile assertions — use the adapter package instead (ports imports adapters
// would create cycle).
