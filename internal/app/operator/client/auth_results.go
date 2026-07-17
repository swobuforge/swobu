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

// ModelCatalogResult is the response from GET /_swobu/model-catalog.
type ModelCatalogResult struct {
	Deployments              []ModelCatalogDeployment `json:"deployments,omitempty"`
	Error                    string                   `json:"error,omitempty"`
	ResolvedProviderProtocol string                   `json:"resolved_provider_protocol,omitempty"`
}

// ModelCatalogDeployment is one deployment returned by the catalog probe.
type ModelCatalogDeployment struct {
	Name                       string   `json:"name"`
	ModelName                  string   `json:"model_name"`
	ModelPublisher             string   `json:"model_publisher,omitempty"`
	ModelVersion               string   `json:"model_version,omitempty"`
	Family                     string   `json:"family,omitempty"`
	SupportedProviderProtocols []string `json:"supported_provider_protocols,omitempty"`
	DefaultProviderProtocol    string   `json:"default_provider_protocol,omitempty"`
}
