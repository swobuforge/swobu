package groq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

const serviceTierAutoMarker = "groq.service_tier_auto"

// NewRuntime composes Groq's shared Responses and Chat grammars with its
// documented Chat-only reasoning and pre-execution Flex-capacity behavior.
// Responses deliberately retain the standard stateless continuation policy.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecGroq))
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	return bundle
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	switch target.ProtocolKind {
	case protocolkind.ChatCompletions:
		backend.Codec = protocolcodec.Codec{
			Protocol: protocolkind.ChatCompletions,
			ChatDialect: protocolcodec.ChatDialect{
				LowerTool:         protocolcodec.ChatHostedSearchTool(nil, "browser_search"),
				LowerToolPolicy:   protocolcodec.ChatHostedSearchToolPolicy("browser_search"),
				LowerReasoning:    applyGroqReasoning,
				DecorateAttempt:   decorateGroqAttempt,
				ResponseReasoning: func() protocolcodec.ChatReasoningExtractor { return groqChatReasoningExtractor{} },
			},
		}
		backend.Transport = capacityTransport{standard: backend.Transport}
	case protocolkind.Responses:
		// The zero-value standard codec does not capture provider response IDs,
		// forcing Exchange to materialize canonical full history.
		backend.Codec = protocolcodec.Codec{
			Protocol: protocolkind.Responses,
		}
	default:
		return provider.Backend{}, fmt.Errorf("Groq backend protocol %q is unsupported", target.ProtocolKind)
	}
	return backend, backend.Validate()
}

func applyGroqReasoning(req canonical.CanonicalRequest, changeLog *[]compat.Change, exchangeID string) (map[string]any, error) {
	controls := req.Controls()
	reasoning := req.Reasoning()
	fields := make(map[string]any)
	if effort, specified := controls.Effort.Get(); specified {
		switch effort {
		case canonical.InferenceEffortLow, canonical.InferenceEffortMedium, canonical.InferenceEffortHigh:
			fields["reasoning_effort"] = string(effort)
		default:
			// Groq documents only these three provider-wide ordinal spellings.
			// Do not guess a model-family conversion for the remaining canonicals.
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestControlsEffort, canonical.Occurrence{}))
			}
		}
	}
	if disclosure, specified := reasoning.DisclosureField().Get(); specified && disclosure == canonical.ReasoningDisclosureNone {
		fields["include_reasoning"] = false
	}
	return fields, nil
}

func decorateGroqAttempt(ctx protocolcodec.AttemptContext) (protocolcodec.AttemptDecoration, error) {
	if !ctx.HasNextRouteCandidate {
		return protocolcodec.AttemptDecoration{}, nil
	}
	return protocolcodec.AttemptDecoration{
		Fields: map[string]any{"service_tier": "auto"},
		Meta:   carrier.Meta{Opaque: map[string]string{serviceTierAutoMarker: "true"}},
	}, nil
}

// capacityTransport establishes a pre-execution fact only for Groq's exact
// Flex signal and only when this codec actually requested the fallback-friendly
// service tier. All other backend errors retain conservative shared handling.
type capacityTransport struct{ standard provider.Transport }

func (t capacityTransport) Send(ctx context.Context, document carrier.Document) (provider.Ingress, error) {
	ingress, err := t.standard.Send(ctx, document)
	if err == nil || !isMarkedServiceTierAuto(document) || !isCapacityExceeded(err) {
		return ingress, err
	}
	return ingress, provider.AttemptRejectedBeforeExecution(provider.Rejected(attemptCause(err)))
}

func isMarkedServiceTierAuto(document carrier.Document) bool {
	if document.Meta.Opaque[serviceTierAutoMarker] != "true" {
		return false
	}
	var payload struct {
		ServiceTier string `json:"service_tier"`
	}
	return json.Unmarshal(document.RawBytes(), &payload) == nil && payload.ServiceTier == "auto"
}

func isCapacityExceeded(err error) bool {
	var backend canonical.BackendError
	if !errors.As(err, &backend) || backend.StatusCode != 498 {
		return false
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	return json.Unmarshal([]byte(backend.Message), &envelope) == nil && envelope.Error.Code == "capacity_exceeded"
}

func attemptCause(err error) error {
	if failure, ok := provider.AsAttemptFailure(err); ok {
		return failure.Cause()
	}
	return err
}

var _ provider.Transport = capacityTransport{}
