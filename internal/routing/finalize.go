package routing

import (
	"fmt"
	"strings"
)

// TargetDraft is the raw, boundary-neutral input finalized into one Target.
// Transport and persistence adapters populate it without interpreting provider
// compatibility or connection modes.
type TargetDraft struct {
	ID         string
	Model      string
	Protocol   string
	Connection ConnectionDraft
}

// ConnectionDraft is a raw tagged union. Exactly one arm must be present.
type ConnectionDraft struct {
	APIKey  *APIKeyConnectionDraft
	ZAI     *ZAIConnectionDraft
	Ollama  *OllamaConnectionDraft
	Azure   *AzureConnectionDraft
	Bedrock *BedrockConnectionDraft
	Custom  *CustomConnectionDraft
}

// APIKeyConnectionDraft carries one fixed provider identity and unresolved
// credential locator from a provider-specific transport arm.
type APIKeyConnectionDraft struct {
	Provider   Provider
	Credential string
}

// ZAIConnectionDraft carries the selected Z.AI access product and credential.
type ZAIConnectionDraft struct {
	Access     string
	Credential string
}

// OllamaConnectionDraft carries an optional local base URL.
type OllamaConnectionDraft struct{ BaseURL string }

// AzureConnectionDraft carries the project endpoint before catalog normalization.
type AzureConnectionDraft struct {
	ProjectEndpoint string
	Credential      string
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

// TargetConstructionFacts supplies catalog facts at the construction edge without
// importing provider catalogs into the routing domain.
type TargetConstructionFacts struct {
	ProtocolSupported             ProtocolSupport
	DerivedProtocol               func(Provider) (string, bool)
	NormalizeAzureProjectEndpoint func(string) (string, error)
	BedrockRegionSupported        func(string) bool
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

// FinalizeConnection validates one raw transport connection union without
// requiring unrelated target identity, model, or protocol fields.
func FinalizeConnection(draft ConnectionDraft, facts TargetConstructionFacts) (Connection, error) {
	count := 0
	for _, present := range []bool{draft.APIKey != nil, draft.ZAI != nil, draft.Ollama != nil, draft.Azure != nil, draft.Bedrock != nil, draft.Custom != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return nil, pathError("target.connection", "exactly one provider variant is required")
	}
	if draft.APIKey != nil {
		return NewAPIKeyConnection(draft.APIKey.Provider, draft.APIKey.Credential)
	}
	if draft.ZAI != nil {
		access, err := ParseZAIAccess(draft.ZAI.Access)
		if err != nil {
			return nil, err
		}
		return NewZAIConnection(access, draft.ZAI.Credential)
	}
	if draft.Ollama != nil {
		return NewOllamaConnection(draft.Ollama.BaseURL)
	}
	if draft.Azure != nil {
		if facts.NormalizeAzureProjectEndpoint == nil {
			return nil, pathError("connection.azure.project_endpoint", "Azure endpoint normalization capability is required")
		}
		endpoint, err := facts.NormalizeAzureProjectEndpoint(draft.Azure.ProjectEndpoint)
		if err != nil {
			return nil, err
		}
		return NewAzureConnection(endpoint, draft.Azure.Credential)
	}
	if draft.Bedrock != nil {
		if facts.BedrockRegionSupported == nil || !facts.BedrockRegionSupported(draft.Bedrock.Region) {
			return nil, pathError("connection.bedrock.region", fmt.Sprintf("unsupported region %q", draft.Bedrock.Region))
		}
		region, err := ParseBedrockRegion(draft.Bedrock.Region)
		if err != nil {
			return nil, err
		}
		return NewBedrockConnection(region, draft.Bedrock.Endpoint, draft.Bedrock.Credential)
	}
	var auth CustomAuth
	if draft.Custom.Header != nil {
		header, err := NewCustomHeaderAuth(draft.Custom.Header.Name, draft.Custom.Header.Credential)
		if err != nil {
			return nil, err
		}
		auth = header
	}
	return NewCustomConnection(draft.Custom.BaseURL, auth)
}
