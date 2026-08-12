package scaleway

import (
	"context"
	"fmt"
	"net/http"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

// NewRuntime composes Scaleway's OpenAI-compatible transport and catalog with
// the two small documented differences in its stateless Responses and Chat
// reasoning carriers. A base URL and credential are target facts, not modes.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecScaleway))
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
		return provider.Backend{}, fmt.Errorf("Scaleway backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	switch target.ProtocolKind {
	case protocolkind.ChatCompletions:
		backend.Codec = reasoningCodec{standard: standard}
	case protocolkind.Responses:
		backend.Codec = responsesCodec{standard: standard}
	default:
		return provider.Backend{}, fmt.Errorf("Scaleway backend protocol %q is unsupported", target.ProtocolKind)
	}
	return backend, backend.Validate()
}

// responsesCodec keeps the standard full-history projection. It only maps the
// three carriers Scaleway deliberately does not expose: top-level
// instructions, explicit store:false, and include. Other fields remain visible
// to the endpoint.
type responsesCodec struct{ standard protocolcodec.Codec }

func (c responsesCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	var changes []compat.Change
	document, err := responses.LowerProviderRequestDocument(
		responses.EncodeInput{Request: req.Canonical, ToolNames: req.ToolNames}, req.Delivery, &changes, req.ExchangeID, responses.EncodeOptions{},
	)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	if instructions, _ := document.Payload["instructions"].(string); instructions != "" {
		input, err := prependInstruction(document.Input, instructions)
		if err != nil {
			return carrier.Document{}, changes, err
		}
		document.Input, document.InputSpecified = input, true
	}
	delete(document.Payload, "instructions")
	delete(document.Payload, "include")
	if document.Store != nil && !*document.Store {
		document.Store = nil
	}
	encoded, err := responses.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

func (c responsesCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.standard.Decode(ctx, req, ingress)
}

func prependInstruction(input any, instructions string) ([]any, error) {
	instruction := responsesMessage("system", instructions)
	switch value := input.(type) {
	case string:
		return []any{instruction, responsesMessage("user", value)}, nil
	case []any:
		return append([]any{instruction}, value...), nil
	case nil:
		return []any{instruction}, nil
	default:
		return nil, canonical.InternalError("Scaleway Responses input has an unsupported typed shape")
	}
}

func responsesMessage(role, text string) map[string]any {
	return map[string]any{
		"type": "message", "role": role,
		"content": []map[string]string{{"type": "input_text", "text": text}},
	}
}

var _ provider.Codec = responsesCodec{}
