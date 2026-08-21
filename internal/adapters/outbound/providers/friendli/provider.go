package friendli

import (
	"context"
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

// NewRuntime composes Friendli's endpoint-agnostic Bearer transport with the
// shared Chat Completions, Responses, and Messages codecs. Friendli model
// tokens name materially different deployment resources, so model discovery is
// intentionally unavailable and operators retain exact manual entry.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecFriendli))
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	bundle.Discovery = unsupportedDiscovery{}
	return bundle
}

type unsupportedDiscovery struct{}

func (unsupportedDiscovery) ProbeTarget(context.Context, provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	return provider.TargetProbeResult{}, canonical.NotImplemented("Swobu does not implement Friendli model discovery")
}

// backendResolver decorates only the shared Chat grammar. Responses and
// Messages stay exact standard-codec composition because Friendli has no
// demonstrated differing wire semantics for those protocol families.
type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil || target.ProtocolKind != protocolkind.ChatCompletions {
		return backend, err
	}
	backend.Codec = protocolcodec.Codec{
		Protocol: protocolkind.ChatCompletions,
		ChatDialect: protocolcodec.ChatDialect{
			LowerReasoning: func(req canonical.CanonicalRequest, changeLog *[]compat.Change, exchangeID string) (map[string]any, error) {
				fields := make(map[string]any)
				if disclosure, set := req.Reasoning().DisclosureField().Get(); set && disclosure == canonical.ReasoningDisclosureNone {
					fields["parse_reasoning"] = true
					fields["include_reasoning"] = false
				}
				return fields, nil
			},
			ResponseReasoning: func() protocolcodec.ChatReasoningExtractor { return friendliChatReasoningExtractor{} },
		},
	}
	return backend, backend.Validate()
}

var _ provider.Discovery = unsupportedDiscovery{}
