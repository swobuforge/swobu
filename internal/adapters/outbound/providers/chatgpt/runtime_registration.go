package chatgpt

import (
	"net/http"

	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
)

func init() {
	providersruntime.RegisterRuntimeFactory(profile.ProviderSpecChatGPT, func(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
		return NewRuntime(profile.ProviderSpecChatGPT, client, credentials)
	})
}
