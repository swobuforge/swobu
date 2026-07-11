package runtime

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

// ResponseDecoder converts one backend success body into one provider response.
type ResponseDecoder func(stream carrier.WireStream) (ports.ProviderResponseStream, error)

// SelectResponseDecoder returns the streaming or buffered decoder for one
// delivery mode. Caller owns error semantics when no decoder is selected.
func SelectResponseDecoder(mode delivery.Mode, streaming ResponseDecoder, buffered ResponseDecoder) (ResponseDecoder, bool) {
	if mode == delivery.Streaming {
		if streaming == nil {
			return nil, false
		}
		return streaming, true
	}
	if buffered == nil {
		return nil, false
	}
	return buffered, true
}

// RequireProviderAndProtocol validates provider and protocol-kind ownership for one executor path.
func RequireProviderAndProtocol(
	providerIDRaw string,
	expectedProviderID profile.ProviderID,
	actualProtocolKind protocolkind.ProtocolKind,
	expectedProtocolKind protocolkind.ProtocolKind,
	providerName string,
) error {
	providerID, ok := profile.ParseProviderID(providerIDRaw)
	if !ok || providerID != expectedProviderID {
		return canonical.BadEndpoint(providerName + " provider id is unsupported")
	}
	if actualProtocolKind != expectedProtocolKind {
		return canonical.UnsupportedOperation(providerName + " provider requires " + string(expectedProtocolKind) + " protocol")
	}
	return nil
}
