package commandcode

import (
	"net/http"
	"strings"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
)

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	policy := openaifamily.StandardBearerPolicy(profile.ProviderSpecCommandCode).WithModelCatalogProject(projectModel)
	return openaifamily.NewRuntime(client, credentials, policy)
}

func projectModel(providerID profile.ProviderID, row modelcatalogopenai.ModelRow) (profile.ModelAuthoringOption, bool, error) {
	protocol, err := commandCodeProtocol(row)
	if err != nil {
		return profile.ModelAuthoringOption{}, false, err
	}
	return profile.NewModelAuthoringOption(row.ID(), row.ID(), string(providerID), "", "", []string{protocol, protocol + "_stream"}, protocol), true, nil
}

func commandCodeProtocol(row modelcatalogopenai.ModelRow) (string, error) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(row.ID())), "claude-") {
		return "messages", nil
	}
	return "chat_completions", nil
}
