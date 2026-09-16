// Package vercel composes Vercel AI Gateway's Messages version header and
// language-only model authoring projection with the shared OpenAI-family runtime.
package vercel

import (
	"encoding/json"
	"net/http"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/profile"
)

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	policy := openaifamily.BearerWithMessagesVersionPolicy(profile.ProviderSpecVercel).
		WithModelCatalogProject(projectModel)
	return openaifamily.NewRuntime(client, credentials, policy)
}

func projectModel(_ profile.ProviderID, row modelcatalogopenai.ModelRow) (profile.ModelAuthoringOption, bool, error) {
	var metadata struct {
		Type    json.RawMessage `json:"type"`
		OwnedBy json.RawMessage `json:"owned_by"`
	}
	if err := json.Unmarshal(row.RawJSON(), &metadata); err != nil {
		return profile.ModelAuthoringOption{}, false, nil
	}
	modelType, ok := optionalString(metadata.Type)
	if !ok || modelType != "language" {
		return profile.ModelAuthoringOption{}, false, nil
	}
	publisher, _ := optionalString(metadata.OwnedBy)
	return profile.NewModelAuthoringOption(
		row.ID(),
		row.ID(),
		publisher,
		"",
		"",
		nil,
		"",
	), true, nil
}

func optionalString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}
