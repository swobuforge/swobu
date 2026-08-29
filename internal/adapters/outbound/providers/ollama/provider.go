// Package ollama composes Ollama's OpenAI-compatible runtime with its narrow
// Responses request-grammar normalization.
package ollama

import (
	"net/http"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// NewRuntime conservatively places current Responses instructions before full
// replay history for every Ollama target. This provider-wide normalization
// avoids model-name inference while preserving ordinary OpenAI-family behavior
// for Chat Completions and other providers.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardNoAuthPolicy(profile.ProviderSpecOllama))
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	return bundle
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	if target.ProtocolKind == protocolkind.Responses {
		backend.Codec = protocolcodec.Codec{
			Protocol: protocolkind.Responses,
			ResponsesDialect: protocolcodec.ResponsesDialect{
				PrependInstructionsToInput: true,
				HistoryMessageRole:         lowerHistoryDirectiveRole,
				Tools: protocolcodec.ResponsesToolLowering{
					Custom: protocolcodec.ResponsesCustomAsFunction(),
				},
			},
		}
	}
	return backend, backend.Validate()
}

// lowerHistoryDirectiveRole preserves the position and content of a late
// directive while avoiding Ollama's provider-wide requirement that system
// messages appear only at the beginning. Hoisting would change activation
// order, so the exact occurrence becomes a user-role approximation instead.
func lowerHistoryDirectiveRole(index int, role canonical.MessageRole) (canonical.MessageRole, []compat.Change, error) {
	if role != canonical.MessageRoleSystem && role != canonical.MessageRoleDeveloper {
		return role, nil, nil
	}
	return canonical.MessageRoleUser, []compat.Change{
		compat.NewApproximation(canonical.RequestItemsMessageRole, canonical.RequestItemOccurrence(uint32(index))),
	}, nil
}
