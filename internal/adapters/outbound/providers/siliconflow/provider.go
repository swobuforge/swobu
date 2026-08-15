package siliconflow

import (
	"net/http"
	"net/url"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
)

// NewRuntime composes SiliconFlow's shared Bearer Chat/Messages runtime with
// its provider-owned filtered model catalog.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	policy := openaifamily.StandardBearerPolicy(profile.ProviderSpecSiliconFlow).
		WithModelCatalogQuery(func(query url.Values) {
			query.Set("type", "text")
			query.Set("sub_type", "chat")
		})
	return openaifamily.NewRuntime(client, credentials, policy)
}
