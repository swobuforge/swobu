package nous

import (
	"net/http"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
)

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	policy := openaifamily.StandardBearerPolicy(profile.ProviderSpecNous).WithModelCatalogProject(projectModel)
	return openaifamily.NewRuntime(client, credentials, policy)
}

func projectModel(providerID profile.ProviderID, row modelcatalogopenai.ModelRow) (profile.ModelAuthoringOption, bool, error) {
	return profile.NewModelAuthoringOption(row.ID(), row.ID(), string(providerID), "", "", []string{"chat_completions", "chat_completions_stream"}, "chat_completions"), true, nil
}
