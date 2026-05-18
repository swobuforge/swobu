package runtime

import (
	"io"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

// ResponseDecoder converts one backend success body into one provider response.
type ResponseDecoder func(body io.ReadCloser) (ports.ProviderResponse, error)

// SelectResponseDecoder returns the streaming or buffered decoder for one
// delivery mode. Caller owns error semantics when no decoder is selected.
func SelectResponseDecoder(delivery bool, streaming ResponseDecoder, buffered ResponseDecoder) (ResponseDecoder, bool) {
	if delivery {
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
	expectedProviderID providercatalog.ProviderID,
	actualProtocolKind protocolkind.ProtocolKind,
	expectedProtocolKind protocolkind.ProtocolKind,
	providerName string,
) error {
	providerID, ok := providercatalog.ParseProviderID(providerIDRaw)
	if !ok || providerID != expectedProviderID {
		return canonical.BadEndpoint(providerName + " provider id is unsupported")
	}
	if actualProtocolKind != expectedProtocolKind {
		return canonical.UnsupportedOperation(providerName + " provider requires " + string(expectedProtocolKind) + " protocol")
	}
	return nil
}
