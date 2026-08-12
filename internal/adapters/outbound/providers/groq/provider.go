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
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
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
	standard, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, fmt.Errorf("Groq backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	switch target.ProtocolKind {
	case protocolkind.ChatCompletions:
		backend.Codec = chatCodec{standard: standard}
		backend.Transport = capacityTransport{standard: backend.Transport}
	case protocolkind.Responses:
		// The zero-value standard codec does not capture provider response IDs,
		// while this wrapper rejects an accidental native-continuation input.
		// Together they force Exchange to materialize canonical full history.
		backend.Codec = responsesCodec{standard: standard}
	default:
		return provider.Backend{}, fmt.Errorf("Groq backend protocol %q is unsupported", target.ProtocolKind)
	}
	return backend, backend.Validate()
}

// chatCodec adds only Groq's provider-wide Chat request carriers after shared
// typed lowering. Model-specific fields, including reasoning_format, never
// enter this adapter because catalog identity is not capability authority.
type chatCodec struct{ standard protocolcodec.Codec }

func (c chatCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	var changes []compat.Change
	document, err := chatcompletions.LowerProviderRequestDocument(req.Canonical, req.ToolNames, req.Delivery, &changes, req.ExchangeID)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	if effort, specified := req.Canonical.Controls().Effort.Get(); specified {
		switch effort {
		case canonical.InferenceEffortLow, canonical.InferenceEffortMedium, canonical.InferenceEffortHigh:
			document.Payload["reasoning_effort"] = string(effort)
		default:
			// Groq documents only these three provider-wide ordinal spellings.
			// Do not guess a model-family conversion for the remaining canonicals.
			changes = compat.AppendUnique(changes, compat.NewOmission(canonical.RequestControlsEffort, canonical.Occurrence{}))
		}
	}
	if disclosure, specified := req.Canonical.Reasoning().DisclosureField().Get(); specified && disclosure == canonical.ReasoningDisclosureNone {
		document.Payload["include_reasoning"] = false
	}
	if req.EncodeContext.HasNextRouteCandidate {
		document.Payload["service_tier"] = "auto"
	}
	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	if err == nil && req.EncodeContext.HasNextRouteCandidate {
		encoded.Meta.Opaque = map[string]string{serviceTierAutoMarker: "true"}
	}
	return encoded, changes, err
}

func (c chatCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.standard.Decode(ctx, req, ingress)
}

// responsesCodec preserves shared Responses grammar while making Groq's
// documented stateless multi-turn rule structural at the adapter edge.
type responsesCodec struct{ standard protocolcodec.Codec }

func (c responsesCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	req.ResponsesPrevious = nil
	return c.standard.Encode(req)
}

func (c responsesCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.standard.Decode(ctx, req, ingress)
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

var _ provider.Codec = chatCodec{}
var _ provider.Codec = responsesCodec{}
var _ provider.Transport = capacityTransport{}
