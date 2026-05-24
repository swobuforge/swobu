package requestpath

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

func resolveProviderProtocolForRequest(providerSpec string, configured protocolkind.ProtocolKind, request canonical.CanonicalRequest) (protocolkind.ProtocolKind, error) {
	return resolveProviderProtocolForRequestWithVariant(providerSpec, "", configured, request)
}

func resolveProviderProtocolForRequestWithVariant(providerSpec string, providerProtocol string, configured protocolkind.ProtocolKind, request canonical.CanonicalRequest) (protocolkind.ProtocolKind, error) {
	_ = configured
	_ = request
	if !providercatalog.SupportsSpec(providerSpec) {
		return "", canonical.BadEndpoint("provider id is unsupported")
	}
	providerProtocol = strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	if providerProtocol == "" || providerProtocol == providercatalog.ProviderProtocolAuto {
		return "", canonical.BadEndpoint("provider protocol must be concrete")
	}
	if protocol, _, ok := providercatalog.ProviderProtocolKindAndFrame(providerSpec, providerProtocol); ok {
		return protocol, nil
	}
	return "", canonical.BadEndpoint("selected provider protocol is unsupported")
}
