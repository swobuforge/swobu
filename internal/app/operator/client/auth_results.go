package operatorclient

// Runtime lane: auth/session result DTOs returned by daemon HTTP APIs.
type AccessCheckResult struct {
	Status  string
	Message string
}

type AuthSessionStartResult struct {
	ProviderSpec string
	SessionID    string
	AuthorizeURL string
	UserCode     string
	ExpiresAt    string
	State        string
}

type AuthSessionStatusResult struct {
	ProviderSpec  string
	SessionID     string
	State         string
	CredentialRef string
	ErrorMessage  string
}

type AuthSessionRetryResult struct {
	SessionID    string
	AuthorizeURL string
	UserCode     string
	ExpiresAt    string
	State        string
}
