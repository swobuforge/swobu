package azure

import (
	"net/http"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
)

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider, azureProjectEndpoint string) providersruntime.ProviderRuntimeBundle {
	return openaifamily.NewRuntime(client, credentials, openaifamily.NewAzurePolicy(), azureProjectEndpoint)
}
