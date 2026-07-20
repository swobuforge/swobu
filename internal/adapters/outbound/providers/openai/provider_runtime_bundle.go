package openai

import (
	"fmt"
	"net/http"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.NewOpenAIPolicy())
	bundle.BackendResolver = chatCompletionsBackendResolver{standard: bundle.BackendResolver}
	return bundle
}

type chatCompletionsBackendResolver struct{ standard provider.BackendResolver }

func (r chatCompletionsBackendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	if target.ProtocolKind == protocolkind.ChatCompletions {
		standard, ok := backend.Codec.(protocolcodec.Codec)
		if !ok {
			return provider.Backend{}, fmt.Errorf("OpenAI chat completions backend has codec %T, want protocolcodec.Codec", backend.Codec)
		}
		backend.Codec = chatCompletionsCodec{Codec: standard}
	}
	return backend, backend.Validate()
}

// chatCompletionsCodec owns OpenAI's max_completion_tokens request spelling.
type chatCompletionsCodec struct{ protocolcodec.Codec }

func (c chatCompletionsCodec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	return c.Codec.EncodeChat(req, func(document *chatcompletions.ProviderRequestDocument) (bool, error) {
		value, ok := document.Payload["max_tokens"]
		if ok {
			delete(document.Payload, "max_tokens")
			document.Payload["max_completion_tokens"] = value
		}
		return false, nil
	})
}

var _ provider.Codec = chatCompletionsCodec{}
