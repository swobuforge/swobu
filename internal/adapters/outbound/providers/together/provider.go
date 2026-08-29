package together

import (
	"fmt"
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// NewRuntime composes Together's fixed Chat Completions transport, unified
// model picker, and documented reasoning dialect around shared protocol work.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecTogether))
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	bundle.Discovery = newDiscovery(client, credentials)
	return bundle
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	if target.ProtocolKind != protocolkind.ChatCompletions {
		return provider.Backend{}, fmt.Errorf("Together AI backend protocol %q is not Chat Completions", target.ProtocolKind)
	}
	backend.Codec = protocolcodec.Codec{
		Protocol: protocolkind.ChatCompletions,
		ChatDialect: protocolcodec.ChatDialect{
			LowerReasoning: func(req canonical.CanonicalRequest, target protocolcodec.ReasoningTargetDialect, changeLog *[]compat.Change, exchangeID string) (map[string]any, error) {
				fields := make(map[string]any)
				compute, computeSet := req.Reasoning().ComputeField().Get()
				if computeSet {
					switch compute.Kind() {
					case canonical.ReasoningDisabled:
						if target.ProjectDisabled(changeLog) {
							fields["reasoning"] = map[string]bool{"enabled": false}
						}
					case canonical.ReasoningAutomatic, canonical.ReasoningBudget:
						fields["reasoning"] = map[string]bool{"enabled": true}
					}
				}
				if effort, effortSet := req.Controls().Effort.Get(); effortSet {
					fields["reasoning_effort"] = string(target.ProjectEffort(effort, changeLog))
				}
				return fields, nil
			},
			ResponseReasoning: func() protocolcodec.ChatReasoningExtractor { return togetherChatReasoningExtractor{} },
		},
	}
	return backend, backend.Validate()
}
