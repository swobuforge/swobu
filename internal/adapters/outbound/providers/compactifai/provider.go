package compactifai

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

// NewRuntime retains shared Bearer auth, transport, discovery, and OpenAI wire
// grammars while projecting CompactifAI's documented per-model capabilities.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	policy := openaifamily.StandardBearerPolicy(profile.ProviderSpecCompactifAI).WithModelCatalogProject(projectModel)
	bundle := openaifamily.NewRuntime(client, credentials, policy)
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	return bundle
}

type compactifAIModel struct {
	OwnedBy      string `json:"owned_by"`
	Capabilities struct {
		SupportChatCompletion bool `json:"support_chat_completion"`
		SupportsResponses     bool `json:"supports_responses"`
	} `json:"capabilities"`
}

func projectModel(providerID profile.ProviderID, row modelcatalogopenai.ModelRow) (profile.ModelAuthoringOption, bool, error) {
	var model compactifAIModel
	if err := json.Unmarshal(row.RawJSON(), &model); err != nil {
		return profile.ModelAuthoringOption{}, false, err
	}
	protocols := make([]string, 0, 4)
	if model.Capabilities.SupportsResponses {
		protocols = append(protocols, "responses", "responses_stream")
	}
	if model.Capabilities.SupportChatCompletion {
		protocols = append(protocols, "chat_completions", "chat_completions_stream")
	}
	if len(protocols) == 0 {
		return profile.ModelAuthoringOption{}, false, nil
	}
	defaultProtocol := "responses"
	if model.Capabilities.SupportChatCompletion {
		defaultProtocol = "chat_completions"
	}
	return profile.NewModelAuthoringOption(row.ID(), row.ID(), model.OwnedBy, "", "", protocols, defaultProtocol), true, nil
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	if target.ProtocolKind != protocolkind.ChatCompletions {
		if target.ProtocolKind != protocolkind.Responses || !target.ProviderDelivery.IsStreaming() {
			return backend, nil
		}
		standard, ok := backend.Codec.(protocolcodec.Codec)
		if !ok {
			return provider.Backend{}, fmt.Errorf("CompactifAI Responses backend has codec %T, want protocolcodec.Codec", backend.Codec)
		}
		backend.Codec = responsesCodec{standard: standard}
		return backend, backend.Validate()
	}
	standard, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, fmt.Errorf("CompactifAI Chat backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	backend.Codec = reasoningCodec{standard: standard}
	return backend, backend.Validate()
}
