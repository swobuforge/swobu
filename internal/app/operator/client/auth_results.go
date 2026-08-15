package operatorclient

import "encoding/json"

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

// ModelCatalogResult is the response from POST /_swobu/target-probe.
type ModelCatalogResult struct {
	Options                  []ModelAuthoringOption `json:"deployments,omitempty"`
	Error                    string                 `json:"error,omitempty"`
	ResolvedProviderProtocol string                 `json:"resolved_provider_protocol,omitempty"`
	Diagnostics              json.RawMessage        `json:"diagnostics,omitempty"`
}

// ModelAuthoringOption is one advisory model-authoring option returned by the
// catalog probe. The containing result retains the public JSON key
// "deployments" for operator API compatibility.
type ModelAuthoringOption struct {
	Name                       string   `json:"name"`
	ModelName                  string   `json:"model_name"`
	ModelPublisher             string   `json:"model_publisher,omitempty"`
	ModelVersion               string   `json:"model_version,omitempty"`
	Family                     string   `json:"family,omitempty"`
	SupportedProviderProtocols []string `json:"supported_provider_protocols,omitempty"`
	DefaultProviderProtocol    string   `json:"default_provider_protocol,omitempty"`
}
