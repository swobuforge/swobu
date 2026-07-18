package authplane

import "context"

type SessionState string

const (
	SessionStatePending   SessionState = "pending"
	SessionStateSucceeded SessionState = "succeeded"
	SessionStateFailed    SessionState = "failed"
	SessionStateExpired   SessionState = "expired"
	SessionStateCanceled  SessionState = "canceled"
)

type StartInput struct {
	ProviderSpec string
	Workspace    string
	Route        string
	TargetID     string
	DraftSubject string
	AuthMode     string
}

type CredentialSubject struct {
	Workspace    string
	Route        string
	TargetID     string
	DraftSubject string
}

type StartOutput struct {
	SessionID    string
	State        SessionState
	AuthorizeURL string
	UserCode     string
	ExpiresAt    string
}

type SessionOutput struct {
	ProviderSpec  string
	SessionID     string
	State         SessionState
	CredentialRef string
	ErrorMessage  string
}

type DriverStartResult struct {
	SessionID    string
	AuthorizeURL string
	UserCode     string
	ExpiresAt    string
}

type DriverPollResult struct {
	State         SessionState
	CredentialRef string
	ErrorMessage  string
}

type AuthMethodDriver interface {
	Start(ctx context.Context, in StartInput) (DriverStartResult, error)
	Poll(ctx context.Context, sessionID string) (DriverPollResult, error)
	Cancel(ctx context.Context, sessionID string) error
}

type CredentialStore interface {
	SetCredential(ctx context.Context, subject CredentialSubject, credentialRef string) (string, error)
}
