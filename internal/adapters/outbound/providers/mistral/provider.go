package mistral

import (
	"encoding/json"
	"fmt"
	"net/http"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// NewRuntime composes Mistral's derived Chat transport, effective-base model
// catalog, and exact reasoning carrier around shared OpenAI-family mechanics.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	policy := openaifamily.StandardBearerPolicy(profile.ProviderSpecMistral).
		WithModelCatalogProject(projectModelRow)
	bundle := openaifamily.NewRuntime(client, credentials, policy)
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	return bundle
}

func projectModelRow(providerID profile.ProviderID, row modelcatalogopenai.ModelRow) (profile.ModelAuthoringOption, bool, error) {
	var model mistralModelCard
	if err := json.Unmarshal(row.RawJSON(), &model); err != nil {
		return profile.ModelAuthoringOption{}, false, err
	}
	// These fields improve authoring only. The selected inference endpoint,
	// never catalog metadata, remains runtime capability authority.
	if model.Archived || !model.Capabilities.CompletionChat {
		return profile.ModelAuthoringOption{}, false, nil
	}
	return profile.NewModelAuthoringOption(row.ID(), row.ID(), "Mistral AI", "", "", nil, ""), true, nil
}

type mistralModelCard struct {
	ID           string `json:"id"`
	Archived     bool   `json:"archived"`
	Capabilities struct {
		CompletionChat bool `json:"completion_chat"`
	} `json:"capabilities"`
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	if target.ProtocolKind != protocolkind.ChatCompletions {
		return provider.Backend{}, fmt.Errorf("Mistral backend protocol %q is not Chat Completions", target.ProtocolKind)
	}
	standard, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, fmt.Errorf("Mistral Chat backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	backend.Codec = reasoningCodec{standard: standard}
	return backend, backend.Validate()
}
