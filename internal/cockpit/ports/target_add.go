package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/routing"
)

// TargetSetupQueries reads provider options and model catalogs for the
// add-target workflow.
//
// Adapter ownership: LiveOperatorAdapter must project from profile catalog
// and operator client target probe.
type TargetSetupQueries interface {
	ProbeProviderModels(ctx context.Context, req ProbeProviderModelsRequest) (readmodel.ModelCatalogReadModel, error)
}

// ProbeProviderModelsRequest carries the same valid connection meaning used by
// target persistence and execution.
type ProbeProviderModelsRequest struct {
	Connection       routing.Connection
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
// session. DraftSubject is used only before a target exists.
type StartAuthSessionRequest struct {
	ProviderSpec string
	Workspace    string
	Route        string
	TargetID     string
	DraftSubject string
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
	// Name is a stable form-owned storage slot, not user-facing input. Repeated
	// paste operations for one target overwrite it instead of leaking entries.
	Name   string
	Secret string
}

// StorePastedCredentialResult returns the persisted credential reference.
type StorePastedCredentialResult struct {
	CredentialRef string
}

// Compile assertions — use the adapter package instead (ports imports adapters
// would create cycle).
