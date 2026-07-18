package routing

import "fmt"

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
	OpenAI     *CredentialConnectionDraft
	Anthropic  *CredentialConnectionDraft
	OpenRouter *CredentialConnectionDraft
	ChatGPT    *CredentialConnectionDraft
	Ollama     *OllamaConnectionDraft
	Azure      *AzureConnectionDraft
	Bedrock    *BedrockConnectionDraft
	Custom     *CustomConnectionDraft
}

// CredentialConnectionDraft carries one unresolved credential locator.
type CredentialConnectionDraft struct{ Credential string }

// OllamaConnectionDraft carries an optional local base URL.
type OllamaConnectionDraft struct{ BaseURL string }

// AzureConnectionDraft carries the project endpoint before catalog normalization.
type AzureConnectionDraft struct {
	ProjectEndpoint string
	Credential      string
}

// BedrockConnectionDraft carries region and one raw authentication arm.
type BedrockConnectionDraft struct {
	Region string
	Auth   BedrockAuthDraft
}

// BedrockAuthDraft is a raw tagged union validated during finalization.
type BedrockAuthDraft struct {
	Profile     *string
	Environment bool
	BearerToken *string
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

// TargetCapabilities supplies catalog facts at the construction edge without
// importing provider catalogs into the routing domain.
type TargetCapabilities struct {
	ProtocolSupported             ProtocolSupport
	NormalizeAzureProjectEndpoint func(string) (string, error)
	BedrockRegionSupported        func(string) bool
}

// FinalizeTarget is the single interpretation path for raw target and
// connection input from persistence and operator transports.
func FinalizeTarget(draft TargetDraft, capabilities TargetCapabilities) (Target, error) {
	id, err := ParseTargetID(draft.ID)
	if err != nil {
		return Target{}, err
	}
	model, err := ParseUpstreamModel(draft.Model)
	if err != nil {
		return Target{}, err
	}
	connection, err := finalizeConnection(draft.Connection, capabilities)
	if err != nil {
		return Target{}, err
	}
	protocol, err := ParseProtocol(draft.Protocol, connection.Provider(), capabilities.ProtocolSupported)
	if err != nil {
		return Target{}, err
	}
	return NewTarget(id, model, protocol, connection)
}

func finalizeConnection(draft ConnectionDraft, capabilities TargetCapabilities) (Connection, error) {
	count := 0
	for _, present := range []bool{draft.OpenAI != nil, draft.Anthropic != nil, draft.OpenRouter != nil, draft.ChatGPT != nil, draft.Ollama != nil, draft.Azure != nil, draft.Bedrock != nil, draft.Custom != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return nil, pathError("target.connection", "exactly one provider variant is required")
	}
	if draft.OpenAI != nil {
		return NewOpenAIConnection(draft.OpenAI.Credential)
	}
	if draft.Anthropic != nil {
		return NewAnthropicConnection(draft.Anthropic.Credential)
	}
	if draft.OpenRouter != nil {
		return NewOpenRouterConnection(draft.OpenRouter.Credential)
	}
	if draft.ChatGPT != nil {
		return NewChatGPTConnection(draft.ChatGPT.Credential)
	}
	if draft.Ollama != nil {
		return NewOllamaConnection(draft.Ollama.BaseURL)
	}
	if draft.Azure != nil {
		if capabilities.NormalizeAzureProjectEndpoint == nil {
			return nil, pathError("connection.azure.project_endpoint", "Azure endpoint normalization capability is required")
		}
		endpoint, err := capabilities.NormalizeAzureProjectEndpoint(draft.Azure.ProjectEndpoint)
		if err != nil {
			return nil, err
		}
		return NewAzureConnection(endpoint, draft.Azure.Credential)
	}
	if draft.Bedrock != nil {
		if capabilities.BedrockRegionSupported == nil || !capabilities.BedrockRegionSupported(draft.Bedrock.Region) {
			return nil, pathError("connection.bedrock.region", fmt.Sprintf("unsupported region %q", draft.Bedrock.Region))
		}
		region, err := ParseBedrockRegion(draft.Bedrock.Region)
		if err != nil {
			return nil, err
		}
		authCount := 0
		if draft.Bedrock.Auth.Profile != nil {
			authCount++
		}
		if draft.Bedrock.Auth.Environment {
			authCount++
		}
		if draft.Bedrock.Auth.BearerToken != nil {
			authCount++
		}
		if authCount != 1 {
			return nil, pathError("connection.bedrock.auth", "exactly one auth variant is required")
		}
		var auth BedrockAuth
		if draft.Bedrock.Auth.Profile != nil {
			auth, err = NewBedrockProfileAuth(*draft.Bedrock.Auth.Profile)
		} else if draft.Bedrock.Auth.Environment {
			auth = BedrockEnvironmentAuth{}
		} else {
			auth, err = NewBedrockBearerTokenAuth(*draft.Bedrock.Auth.BearerToken)
		}
		if err != nil {
			return nil, err
		}
		return NewBedrockConnection(region, auth)
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
