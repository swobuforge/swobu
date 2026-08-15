package runpod

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
)

// NewRuntime composes Runpod's shared OpenAI-family codecs and Bearer
// transport. Enumerable catalog behavior, including 404/405 open fallback,
// remains the shared provider-kernel policy.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	policy := openaifamily.StandardBearerPolicy(profile.ProviderSpecRunPod).
		WithModelCatalogMissingStatuses(http.StatusNotFound, http.StatusMethodNotAllowed)
	return openaifamily.NewRuntime(client, credentials, policy)
}
