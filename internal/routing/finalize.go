package routing

import (
	"fmt"
	"strings"
)

// TargetDraft is raw boundary input finalized into one Target. Transport and
// persistence adapters populate it without interpreting provider compatibility
// or connection semantics.
type TargetDraft struct {
	ID         string
	Model      string
	Protocol   string
	Connection ConnectionDraft
}

// ConnectionDraft is a shape-oriented raw union. Provider identity remains an
// external provider key; profile construction facts select the one required
// durable shape. The finalized Connection still excludes invalid combinations.
type ConnectionDraft struct {
	Provider string
	Standard *StandardConnectionDraft
	ZAI      *ZAIConnectionDraft
	Bedrock  *BedrockConnectionDraft
	Custom   *CustomConnectionDraft
}

// StandardConnectionDraft carries the ordinary locator and credential facts.
// Locator is deliberately not named base URL: Azure's project locator proves
// that authoring grammar and durable connection shape are separate concerns.
type StandardConnectionDraft struct {
	Locator    string
	Credential string
}

// ZAIConnectionDraft carries the selected Z.AI access product and credential.
type ZAIConnectionDraft struct {
	Access     string
	Credential string
}

// BedrockConnectionDraft carries the durable Bedrock region and required
// operator-authored inference endpoint.
type BedrockConnectionDraft struct {
	Region     string
	Endpoint   string
	Credential string
}

// CustomConnectionDraft carries explicit non-hosted endpoint and header auth.
type CustomConnectionDraft struct {
	BaseURL string
	Header  *CustomHeaderDraft
}

// CustomHeaderDraft carries one HTTP header name and credential locator.
type CustomHeaderDraft struct {
	Name       string
	Credential string
}

// TargetConstructionFacts supplies catalog facts at the construction edge
// without importing the provider catalog into the routing domain.
type TargetConstructionFacts struct {
	ProviderSupported            ProviderSupport
	ConnectionShape              func(Provider) (ConnectionShape, bool)
	ValidateStandardConnection   func(Provider, StandardConnectionDraft) (StandardConnectionDraft, error)
	ProtocolSupported            ProtocolSupport
	NormalizeProtocol            func(Provider, string) (string, error)
	DerivedProtocol              func(Provider) (string, bool)
	NormalizeAzureProjectLocator func(string) (string, error)
	BedrockRegionSupported       func(string) bool
}

// FinalizeTarget is the single interpretation path for raw target and
// connection input from persistence and operator transports.
func FinalizeTarget(draft TargetDraft, facts TargetConstructionFacts) (Target, error) {
	id, err := ParseTargetID(draft.ID)
	if err != nil {
		return Target{}, err
	}
	model, err := ParseUpstreamModel(draft.Model)
	if err != nil {
		return Target{}, err
	}
	connection, err := FinalizeConnection(draft.Connection, facts)
	if err != nil {
		return Target{}, err
	}
	rawProtocol := strings.TrimSpace(draft.Protocol)
	if facts.NormalizeProtocol != nil {
		normalized, normalizeErr := facts.NormalizeProtocol(connection.Provider(), rawProtocol)
		if normalizeErr != nil {
			return Target{}, pathError("target.protocol", normalizeErr.Error())
		}
		rawProtocol = normalized
	}
	if facts.DerivedProtocol != nil {
		if derivedProtocol, derived := facts.DerivedProtocol(connection.Provider()); derived {
			if rawProtocol != "" {
				return Target{}, pathError("target.protocol", "provider protocol is derived and must be omitted")
			}
			protocol, err := ParseProtocol(derivedProtocol, connection.Provider(), facts.ProtocolSupported)
			if err != nil {
				return Target{}, err
			}
			return NewTarget(id, model, protocol, connection)
		}
	}
	protocol, err := ParseProtocol(rawProtocol, connection.Provider(), facts.ProtocolSupported)
	if err != nil {
		return Target{}, err
	}
	return NewTarget(id, model, protocol, connection)
}

// FinalizeConnection validates one raw provider-keyed connection document
// against catalog construction facts without encoding catalog membership in
// routing. The only exhaustive switch is over the four durable shapes.
func FinalizeConnection(draft ConnectionDraft, facts TargetConstructionFacts) (Connection, error) {
	provider, err := ParseProvider(draft.Provider, facts.ProviderSupported)
	if err != nil {
		return nil, err
	}
	if facts.ConnectionShape == nil {
		return nil, pathError("connection.provider", "provider connection requirements are unavailable")
	}
	shape, ok := facts.ConnectionShape(provider)
	if !ok {
		return nil, pathError("connection.provider", "provider connection requirements are unsupported")
	}
	count := 0
	for _, present := range []bool{draft.Standard != nil, draft.ZAI != nil, draft.Bedrock != nil, draft.Custom != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return nil, pathError("target.connection", "exactly one provider connection is required")
	}
	switch shape {
	case ConnectionShapeStandard:
		if draft.Standard == nil {
			return nil, pathError("target.connection", "provider connection details are required")
		}
		if facts.ValidateStandardConnection == nil {
			return nil, pathError("connection.provider", "provider connection validation is unavailable")
		}
		standard, err := facts.ValidateStandardConnection(provider, *draft.Standard)
		if err != nil {
			return nil, err
		}
		return NewStandardConnection(provider, standard.Locator, standard.Credential)
	case ConnectionShapeZAI:
		if draft.ZAI == nil {
			return nil, pathError("target.connection", "provider access selection is required")
		}
		access, err := ParseZAIAccess(draft.ZAI.Access)
		if err != nil {
			return nil, err
		}
		return NewZAIConnection(provider, access, draft.ZAI.Credential)
	case ConnectionShapeBedrock:
		if draft.Bedrock == nil {
			return nil, pathError("target.connection", "provider region and endpoint are required")
		}
		if facts.BedrockRegionSupported == nil || !facts.BedrockRegionSupported(draft.Bedrock.Region) {
			return nil, pathError("connection.bedrock.region", fmt.Sprintf("unsupported region %q", draft.Bedrock.Region))
		}
		region, err := ParseBedrockRegion(draft.Bedrock.Region)
		if err != nil {
			return nil, err
		}
		return NewBedrockConnection(provider, region, draft.Bedrock.Endpoint, draft.Bedrock.Credential)
	case ConnectionShapeCustom:
		if draft.Custom == nil {
			return nil, pathError("target.connection", "provider endpoint configuration is required")
		}
		var auth CustomAuth
		if draft.Custom.Header != nil {
			header, err := NewCustomHeaderAuth(draft.Custom.Header.Name, draft.Custom.Header.Credential)
			if err != nil {
				return nil, err
			}
			auth = header
		}
		return NewCustomConnection(provider, draft.Custom.BaseURL, auth)
	default:
		return nil, pathError("connection.provider", "provider connection requirements are unsupported")
	}
}
