package openaifamily

import (
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
)

// ProviderRoutePolicy is the complete route-level contract for one
// OpenAI-family provider binding. It owns only the few facts that alter shared
// HTTP transport or model-catalog realization. Provider codecs own request and
// response grammar; profile owns product and setup facts.
//
// Use the standard constructors for ordinary providers. The two special
// constructors exist because their catalog or Messages header behavior differs
// independently, not because a provider needs a named policy class.
type ProviderRoutePolicy struct {
	providerID           profile.ProviderID
	auth                 AuthStrategy
	modelCatalogDialect  ModelCatalogDialect
	applyProtocolHeaders func(protocolkind.ProtocolKind, string, HeaderSetter)
}

// ProviderID returns the explicit provider composed into this adapter runtime.
func (p ProviderRoutePolicy) ProviderID() profile.ProviderID { return p.providerID }

// AuthStrategy returns the provider's default credential header behavior.
func (p ProviderRoutePolicy) AuthStrategy() AuthStrategy { return p.auth }

// ModelCatalogDialect returns the provider's operator-side model-list grammar.
func (p ProviderRoutePolicy) ModelCatalogDialect() ModelCatalogDialect { return p.modelCatalogDialect }

// ApplyProtocolHeaders applies documented protocol-specific request headers.
func (p ProviderRoutePolicy) ApplyProtocolHeaders(kind protocolkind.ProtocolKind, token string, headers HeaderSetter) {
	if p.applyProtocolHeaders != nil {
		p.applyProtocolHeaders(kind, token, headers)
	}
}

// HeaderSetter is the narrow HTTP-header seam for an exact provider policy.
type HeaderSetter interface{ Set(string, string) }

// ModelCatalogDialect selects the provider-owned model-list wire contract.
// Inference protocol selection remains independent of this operator-side fact.
type ModelCatalogDialect uint8

const (
	ModelCatalogOpenAI ModelCatalogDialect = iota + 1
	ModelCatalogLMStudioV1
)

// StandardBearerPolicy constructs the common OpenAI-family route: Bearer
// authentication, the OpenAI model catalog, and no protocol-specific headers.
func StandardBearerPolicy(providerID profile.ProviderID) ProviderRoutePolicy {
	return ProviderRoutePolicy{
		providerID:          providerID,
		auth:                BearerAuthStrategy(),
		modelCatalogDialect: ModelCatalogOpenAI,
	}
}

// StandardNoAuthPolicy constructs the common unauthenticated OpenAI-family
// route used by local servers such as Ollama.
func StandardNoAuthPolicy(providerID profile.ProviderID) ProviderRoutePolicy {
	return ProviderRoutePolicy{
		providerID:          providerID,
		auth:                NoAuthStrategy(),
		modelCatalogDialect: ModelCatalogOpenAI,
	}
}

// APIKeyPolicy constructs an OpenAI-family route that sends an unprefixed
// token in the Azure-style `api-key` header while retaining the OpenAI model
// catalog grammar.
func APIKeyPolicy(providerID profile.ProviderID) ProviderRoutePolicy {
	return ProviderRoutePolicy{
		providerID:          providerID,
		auth:                APIKeyAuthStrategy(),
		modelCatalogDialect: ModelCatalogOpenAI,
	}
}

// LMStudioPolicy retains LM Studio's native `/api/v1/models` catalog with its
// documented fallback to the standard OpenAI catalog endpoint.
func LMStudioPolicy() ProviderRoutePolicy {
	policy := StandardBearerPolicy(profile.ProviderSpecLMStudio)
	policy.modelCatalogDialect = ModelCatalogLMStudioV1
	return policy
}

// GMIPolicy retains GMI's documented Messages authentication headers. Its
// other route behavior is the common Bearer OpenAI-family policy.
func GMIPolicy() ProviderRoutePolicy {
	policy := StandardBearerPolicy(profile.ProviderSpecGMI)
	policy.applyProtocolHeaders = func(kind protocolkind.ProtocolKind, token string, headers HeaderSetter) {
		if kind != protocolkind.Messages || headers == nil {
			return
		}
		headers.Set("X-API-Key", token)
		headers.Set("anthropic-version", "2023-06-01")
	}
	return policy
}
