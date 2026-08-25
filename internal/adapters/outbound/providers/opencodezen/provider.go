package opencodezen

import (
	"net/http"
	"strings"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
)

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	policy := openaifamily.BearerWithMessagesAPIKeyPolicy(profile.ProviderSpecOpenCodeZen).WithModelCatalogProject(projectModel)
	return openaifamily.NewRuntime(client, credentials, policy)
}

func projectModel(providerID profile.ProviderID, row modelcatalogopenai.ModelRow) (profile.ModelAuthoringOption, bool, error) {
	protocol, ok, err := zenProtocol(row)
	if err != nil || !ok {
		return profile.ModelAuthoringOption{}, false, err
	}
	return profile.NewModelAuthoringOption(row.ID(), row.ID(), string(providerID), "", "", protocolContracts(protocol), protocol), true, nil
}

func zenProtocol(row modelcatalogopenai.ModelRow) (string, bool, error) {
	id := strings.ToLower(strings.TrimSpace(row.ID()))
	switch {
	case id == "x-preview-f-free",
		strings.HasPrefix(id, "deepseek-"),
		strings.HasPrefix(id, "glm-"),
		strings.HasPrefix(id, "minimax-"),
		strings.HasPrefix(id, "kimi-"),
		strings.HasPrefix(id, "mimo-"),
		strings.HasPrefix(id, "hy3-"),
		strings.HasPrefix(id, "nemotron-"),
		strings.HasPrefix(id, "big-pickle"):
		return "chat_completions", true, nil
	case strings.HasPrefix(id, "gpt-"), strings.HasPrefix(id, "grok-"), strings.HasPrefix(id, "muse-"):
		return "responses", true, nil
	case strings.HasPrefix(id, "claude-"), strings.HasPrefix(id, "qwen"):
		return "messages", true, nil
	default:
		return "", false, nil
	}
}

func protocolContracts(buffered string) []string {
	return []string{buffered, buffered + "_stream"}
}
