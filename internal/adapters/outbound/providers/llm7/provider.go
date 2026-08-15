package llm7

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
)

// NewRuntime composes LLM7's shared Chat execution with its provider-local
// model catalog. Selectors remain exact authored model identities.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecLLM7))
	bundle.Discovery = discovery{client: client, credentials: credentials}
	return bundle
}
