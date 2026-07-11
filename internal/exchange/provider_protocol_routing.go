package exchange

import (
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
)

// ProviderProtocolRouting is the concrete protocol route selected for one target.
type ProviderProtocolRouting struct {
	Kind     protocolkind.ProtocolKind
	Delivery delivery.Delivery
}

func (r ProviderProtocolRouting) Streaming() bool {
	return r.Delivery.Mode == delivery.Streaming
}

// ResolveProviderProtocolRouting resolves one concrete provider protocol routing
// decision from the selected target.
func ResolveProviderProtocolRouting(target RoutableTarget, missingProtocolMessage string) (ProviderProtocolRouting, error) {
	providerProtocol := strings.TrimSpace(target.ProviderProtocol) // swobu:io-string source=boundary
	if providerProtocol == "" || providerProtocol == profile.ProviderProtocolAuto {
		return ProviderProtocolRouting{}, canonical.BadEndpoint(missingProtocolMessage)
	}
	kind, frame, ok := profile.ProviderProtocolKindAndFrame(target.ProviderID(), providerProtocol)
	if !ok {
		return ProviderProtocolRouting{}, canonical.BadEndpoint("selected provider protocol is unsupported")
	}
	return ProviderProtocolRouting{
		Kind: kind,
		Delivery: func() delivery.Delivery {
			if strings.TrimSpace(frame) != "" { // swobu:io-string source=boundary
				return delivery.StreamingDelivery(delivery.FramingSSE)
			}
			return delivery.BufferedDelivery()
		}(),
	}, nil
}

// ProviderRequestPathForProtocol returns the provider request path for one
// concrete protocol kind.
func ProviderRequestPathForProtocol(kind protocolkind.ProtocolKind) (string, error) {
	switch kind {
	case protocolkind.ChatCompletions:
		return "/chat/completions", nil
	case protocolkind.Responses:
		return "/responses", nil
	case protocolkind.Completions:
		return "/completions", nil
	case protocolkind.Messages:
		return "/messages", nil
	default:
		return "", canonical.UnsupportedOperation("protocol kind is not implemented")
	}
}
