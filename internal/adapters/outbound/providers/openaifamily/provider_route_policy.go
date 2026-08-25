package openaifamily

import (
	"net/url"

	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
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
	modelCatalog         ModelCatalogPolicy
	applyProtocolHeaders func(protocolkind.ProtocolKind, string, HeaderSetter)
}

// ProviderID returns the explicit provider composed into this adapter runtime.
func (p ProviderRoutePolicy) ProviderID() profile.ProviderID { return p.providerID }

// AuthStrategy returns the provider's default credential header behavior.
func (p ProviderRoutePolicy) AuthStrategy() AuthStrategy { return p.auth }

// ModelCatalogDialect returns the provider's operator-side model-list grammar.
func (p ProviderRoutePolicy) ModelCatalogDialect() ModelCatalogDialect { return p.modelCatalogDialect }

// ModelCatalogPolicy returns the authoring-only OpenAI catalog composition for
// this route. It does not describe model support or alter request encoding.
func (p ProviderRoutePolicy) ModelCatalogPolicy() ModelCatalogPolicy { return p.modelCatalog }

// WithModelCatalogQuery adds provider-owned query decoration to the shared
// `/models` request. The hook may only change catalog URL query values.
func (p ProviderRoutePolicy) WithModelCatalogQuery(decorate func(url.Values)) ProviderRoutePolicy {
	p.modelCatalog = p.modelCatalog.WithQuery(decorate)
	return p
}

// WithModelCatalogProject adds provider-owned authoring projection for rows in
// the standard OpenAI catalog envelope.
func (p ProviderRoutePolicy) WithModelCatalogProject(project ModelCatalogProjector) ProviderRoutePolicy {
	p.modelCatalog = p.modelCatalog.WithProjector(project)
	return p
}

// WithModelCatalogMissingStatuses sets the explicit HTTP statuses that mean
// “no advisory catalog” for this route. Other failures remain visible.
func (p ProviderRoutePolicy) WithModelCatalogMissingStatuses(statuses ...int) ProviderRoutePolicy {
	p.modelCatalog = p.modelCatalog.WithMissingStatuses(statuses...)
	return p
}

// ApplyProtocolHeaders applies documented protocol-specific request headers.
func (p ProviderRoutePolicy) ApplyProtocolHeaders(kind protocolkind.ProtocolKind, token string, headers HeaderSetter) {
	if p.applyProtocolHeaders != nil {
		p.applyProtocolHeaders(kind, token, headers)
	}
}

// HeaderSetter is the narrow HTTP-header seam for an exact provider policy.
type HeaderSetter interface{ Set(string, string) }

// ModelCatalogProjector maps one standard OpenAI catalog row into an
// authoring descriptor. Returning false drops that row from the advisory
// catalog; returning an error fails the probe. The projector cannot affect
// request-path capability or protocol selection.
type ModelCatalogProjector func(profile.ProviderID, modelcatalogopenai.ModelRow) (profile.ModelAuthoringOption, bool, error)

// ModelCatalogPolicy composes the small variations of a standard OpenAI
// catalog probe without introducing a provider registry or capability table.
type ModelCatalogPolicy struct {
	query           func(url.Values)
	project         ModelCatalogProjector
	missingStatuses map[int]struct{}
}

// DefaultModelCatalogPolicy returns the common `/models` policy. A provider
// may decorate its query or project complete rows, while 404/405 remain the
// narrow “catalog unavailable” outcome used by enumerable OpenAI surfaces.
func DefaultModelCatalogPolicy() ModelCatalogPolicy {
	return ModelCatalogPolicy{}.WithMissingStatuses(404, 405)
}

func (p ModelCatalogPolicy) WithQuery(decorate func(url.Values)) ModelCatalogPolicy {
	p.query = decorate
	return p
}

func (p ModelCatalogPolicy) WithProjector(project ModelCatalogProjector) ModelCatalogPolicy {
	p.project = project
	return p
}

func (p ModelCatalogPolicy) WithMissingStatuses(statuses ...int) ModelCatalogPolicy {
	p.missingStatuses = make(map[int]struct{}, len(statuses))
	for _, status := range statuses {
		p.missingStatuses[status] = struct{}{}
	}
	return p
}

func (p ModelCatalogPolicy) decorateURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", err
	}
	if p.query != nil {
		values := parsed.Query()
		p.query(values)
		parsed.RawQuery = values.Encode()
	}
	return parsed.String(), nil
}

func (p ModelCatalogPolicy) missingStatus(status int) bool {
	_, ok := p.missingStatuses[status]
	return ok
}

func (p ModelCatalogPolicy) projectRow(providerID profile.ProviderID, row modelcatalogopenai.ModelRow) (profile.ModelAuthoringOption, bool, error) {
	if p.project != nil {
		return p.project(providerID, row)
	}
	return profile.NewModelAuthoringOption(row.ID(), row.ID(), string(providerID), "", string(providerID), nil, ""), true, nil
}

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
		modelCatalog:        DefaultModelCatalogPolicy(),
	}
}

// StandardNoAuthPolicy constructs the common unauthenticated OpenAI-family
// route used by local servers such as Ollama.
func StandardNoAuthPolicy(providerID profile.ProviderID) ProviderRoutePolicy {
	return ProviderRoutePolicy{
		providerID:          providerID,
		auth:                NoAuthStrategy(),
		modelCatalogDialect: ModelCatalogOpenAI,
		modelCatalog:        DefaultModelCatalogPolicy(),
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
		modelCatalog:        DefaultModelCatalogPolicy(),
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
	return BearerWithMessagesAPIKeyPolicy(profile.ProviderSpecGMI)
}

// BearerWithMessagesAPIKeyPolicy retains Bearer authentication for standard
// OpenAI routes and adds the Anthropic-native headers required by a provider's
// Messages compatibility endpoint.
func BearerWithMessagesAPIKeyPolicy(providerID profile.ProviderID) ProviderRoutePolicy {
	policy := StandardBearerPolicy(providerID)
	policy.applyProtocolHeaders = func(kind protocolkind.ProtocolKind, token string, headers HeaderSetter) {
		if kind != protocolkind.Messages || headers == nil {
			return
		}
		headers.Set("X-API-Key", token)
		headers.Set("anthropic-version", "2023-06-01")
	}
	return policy
}

// WorkersAIPolicy adds Cloudflare's required default-gateway header to
// Workers AI generation requests. Discovery is provider-local and manual, so
// this transport policy never decorates a model-catalog probe.
func WorkersAIPolicy() ProviderRoutePolicy {
	policy := StandardBearerPolicy(profile.ProviderSpecWorkersAI)
	policy.applyProtocolHeaders = func(kind protocolkind.ProtocolKind, _ string, headers HeaderSetter) {
		if headers == nil || (kind != protocolkind.ChatCompletions && kind != protocolkind.Responses) {
			return
		}
		headers.Set("cf-aig-gateway-id", "default")
	}
	return policy
}
