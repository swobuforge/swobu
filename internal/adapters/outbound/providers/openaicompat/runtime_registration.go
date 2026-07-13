package openaicompat

import (
	"net/http"

	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
)

func init() {
	providersruntime.RegisterRuntimeFactory(profile.ProviderSpecOpenAICompatible, func(client *http.Client, credentials providersruntime.CredentialProvider, _ string) providersruntime.ProviderRuntimeBundle {
		return NewRuntime(client, credentials)
	})
}
