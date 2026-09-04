package openai

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"unicode/utf8"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

const (
	openAIPromptCacheKeyMaxCharacters = 64
	openAIPromptCacheKeyHashDomain    = "openai-prompt-cache-key:v1"
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
	switch target.ProtocolKind {
	case protocolkind.ChatCompletions:
		backend.Codec = protocolcodec.Codec{
			Protocol: protocolkind.ChatCompletions,
			ChatDialect: protocolcodec.ChatDialect{
				UseMaxCompletionTokens: true,
				DecorateAttempt:        decorateOpenAIAttempt,
			},
		}
	case protocolkind.Responses:
		backend.Codec = protocolcodec.Codec{
			Protocol: protocolkind.Responses,
			ResponsesDialect: protocolcodec.ResponsesDialect{
				CaptureResponsesContinuation: true,
				Tools: protocolcodec.ResponsesToolLowering{
					WebSearch: protocolcodec.ResponsesHostedSearchTool("web_search", true),
				},
				DecorateAttempt: decorateOpenAIAttempt,
			},
		}
	default:
		return provider.Backend{}, fmt.Errorf("OpenAI backend protocol %q is unsupported", target.ProtocolKind)
	}
	return backend, backend.Validate()
}

func decorateOpenAIAttempt(ctx provider.AttemptContext) (protocolcodec.AttemptDecoration, error) {
	if ctx.CacheLocality.IsZero() {
		return protocolcodec.AttemptDecoration{}, nil
	}
	return protocolcodec.AttemptDecoration{
		Fields: map[string]any{"prompt_cache_key": openAIPromptCacheKey(ctx.CacheLocality.Key())},
	}, nil
}

// openAIPromptCacheKey keeps portable cache locality unconstrained until the OpenAI
// edge, whose public request contract accepts at most 64 characters. Hashing
// only oversized values preserves client-owned short keys and gives derived or
// long explicit locality keys one stable, non-revealing provider identity.
func openAIPromptCacheKey(key string) string {
	if utf8.RuneCountInString(key) <= openAIPromptCacheKeyMaxCharacters {
		return key
	}
	sum := sha256.Sum256([]byte(openAIPromptCacheKeyHashDomain + "\x00" + key))
	return hex.EncodeToString(sum[:])
}
