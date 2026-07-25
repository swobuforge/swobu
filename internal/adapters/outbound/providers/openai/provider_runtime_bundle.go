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
	shared "github.com/swobuforge/swobu/internal/wire/shared"
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
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	document, decisions, err := shared.WithAccumulatedDecisions(func(sink compat.Sink) (chatcompletions.ProviderRequestDocument, error) {
		document, err := chatcompletions.LowerProviderRequestDocument(
			req.Canonical,
			req.Delivery,
			sink,
			req.ExchangeID,
			chatcompletions.EncodeOptions{Compatibility: req.Compatibility},
		)
		if err != nil {
			return chatcompletions.ProviderRequestDocument{}, err
		}
		if err := chatcompletions.ApplyStandardProviderRequestReasoning(&document, req.Canonical, sink, req.ExchangeID); err != nil {
			return chatcompletions.ProviderRequestDocument{}, err
		}
		return document, nil
	})
	if err != nil {
		return carrier.Document{}, decisions, err
	}
	if document.MaxTokens != nil {
		document.MaxCompletionTokens = document.MaxTokens
		document.MaxTokens = nil
	}
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	return encoded, decisions, err
}

var _ provider.Codec = chatCompletionsCodec{}
