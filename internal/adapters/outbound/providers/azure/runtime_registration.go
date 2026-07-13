package azure

import (
	"net/http"

	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
)

func init() {
	providersruntime.RegisterRuntimeFactory(profile.ProviderSpecAzure, func(client *http.Client, credentials providersruntime.CredentialProvider, azureProjectEndpoint string) providersruntime.ProviderRuntimeBundle {
		return NewRuntime(client, credentials, azureProjectEndpoint)
	})
}
