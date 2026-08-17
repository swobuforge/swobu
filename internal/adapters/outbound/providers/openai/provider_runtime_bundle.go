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
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecOpenAI))
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
	} else if target.ProtocolKind == protocolkind.Responses {
		standard, ok := backend.Codec.(protocolcodec.Codec)
		if !ok {
			return provider.Backend{}, fmt.Errorf("OpenAI responses backend has codec %T, want protocolcodec.Codec", backend.Codec)
		}
		standard.CaptureResponsesContinuation = true
		backend.Codec = responsesCodec{Codec: standard}
	}
	return backend, backend.Validate()
}

// chatCompletionsCodec owns OpenAI's max_completion_tokens request spelling.
type chatCompletionsCodec struct{ protocolcodec.Codec }

func (c chatCompletionsCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	var changes []compat.Change
	document, err := func(sink *[]compat.Change) (chatcompletions.ProviderRequestDocument, error) {
		document, err := chatcompletions.LowerProviderRequestDocument(
			req.Canonical,
			req.ToolNames,
			req.Delivery,
			sink,
			req.ExchangeID,
		)
		if err != nil {
			return chatcompletions.ProviderRequestDocument{}, err
		}
		if err := chatcompletions.ApplyStandardProviderRequestReasoning(&document, req.Canonical, sink, req.ExchangeID); err != nil {
			return chatcompletions.ProviderRequestDocument{}, err
		}
		return document, nil
	}(&changes)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	if !req.CacheAffinity.IsZero() {
		document.Payload["prompt_cache_key"] = req.CacheAffinity.Key()
	}
	if document.MaxTokens != nil {
		document.MaxCompletionTokens = document.MaxTokens
		document.MaxTokens = nil
	}
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

var _ provider.Codec = chatCompletionsCodec{}

// responsesCodec adds the exact OpenAI locality field while retaining the
// standard codec's continuation-aware response decoding.
type responsesCodec struct{ protocolcodec.Codec }

func (c responsesCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	document, changes, err := protocolcodec.LowerResponsesRequest(req)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	if !req.CacheAffinity.IsZero() {
		document.Payload["prompt_cache_key"] = req.CacheAffinity.Key()
	}
	encoded, err := responses.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

var _ provider.Codec = responsesCodec{}
