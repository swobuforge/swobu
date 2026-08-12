package sambanova

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
	"github.com/swobuforge/swobu/internal/wire/messages"
)

// NewRuntime keeps SambaCloud and SambaStack on the shared protocol and
// transport paths, decorating only the documented Messages thinking carrier.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.StandardBearerPolicy(profile.ProviderSpecSambaNova))
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
	case protocolkind.ChatCompletions, protocolkind.Responses:
		return backend, backend.Validate()
	case protocolkind.Messages:
		standard, ok := backend.Codec.(protocolcodec.Codec)
		if !ok {
			return provider.Backend{}, fmt.Errorf("SambaNova Messages backend has codec %T, want protocolcodec.Codec", backend.Codec)
		}
		backend.Codec = messagesCodec{standard: standard}
		return backend, backend.Validate()
	default:
		return provider.Backend{}, fmt.Errorf("SambaNova backend protocol %q is unsupported", target.ProtocolKind)
	}
}

// messagesCodec omits only enabled/adaptive top-level thinking, a carrier that
// SambaNova documents as unsupported. Disabled thinking and every unrelated
// typed request field remain unchanged.
type messagesCodec struct{ standard protocolcodec.Codec }

func (c messagesCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	var changes []compat.Change
	document, err := messages.LowerProviderRequestDocument(req.Canonical, req.ToolNames, req.Delivery, &changes, req.ExchangeID)
	if err != nil {
		return carrier.Document{}, changes, err
	}
	if thinking, ok := document.Payload["thinking"].(map[string]any); ok {
		if kind, _ := thinking["type"].(string); kind == "enabled" || kind == "adaptive" {
			delete(document.Payload, "thinking")
			changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestReasoning, canonical.RequestReasoning, canonical.Occurrence{}))
		}
	}
	encoded, err := messages.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

func (c messagesCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.standard.Decode(ctx, req, ingress)
}

var _ provider.Codec = messagesCodec{}
