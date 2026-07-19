package routing

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// Connection is a sealed provider-specific sum type. Exactly one concrete arm
// is held by Target, so irrelevant field combinations are unrepresentable.
type Connection interface {
	Provider() Provider
	isConnection()
}

// connectionsEqual compares only durable provider configuration. Keeping this
// rule in the sealed sum prevents runtime-only fields from changing generation.
func connectionsEqual(left, right Connection) bool {
	switch left := left.(type) {
	case OpenAIConnection:
		right, ok := right.(OpenAIConnection)
		return ok && left.credential.String() == right.credential.String()
	case AnthropicConnection:
		right, ok := right.(AnthropicConnection)
		return ok && left.credential.String() == right.credential.String()
	case OpenRouterConnection:
		right, ok := right.(OpenRouterConnection)
		return ok && left.credential.String() == right.credential.String()
	case ChatGPTConnection:
		right, ok := right.(ChatGPTConnection)
		return ok && left.credential.String() == right.credential.String()
	case OllamaConnection:
		right, ok := right.(OllamaConnection)
		return ok && left.baseURL.String() == right.baseURL.String()
	case AzureConnection:
		right, ok := right.(AzureConnection)
		return ok && left.projectEndpoint.String() == right.projectEndpoint.String() && left.credential.String() == right.credential.String()
	case BedrockConnection:
		right, ok := right.(BedrockConnection)
		return ok && left.region == right.region && left.Credential().String() == right.Credential().String()
	case CustomConnection:
		right, ok := right.(CustomConnection)
		return ok && left.baseURL.String() == right.baseURL.String() && customAuthEqual(left.auth, right.auth)
	default:
		return false
	}
}

func customAuthEqual(left, right CustomAuth) bool {
	switch left := left.(type) {
	case CustomHeaderAuth:
		right, ok := right.(CustomHeaderAuth)
		return ok && left.name == right.name && left.credential.String() == right.credential.String()
	default:
		return left == nil && right == nil
	}
}

func parseCredential(path, raw string) (credentialref.Ref, error) {
	ref := credentialref.Parse(raw)
	if ref.String() == "" || ref.Kind() == credentialref.KindOther {
		return credentialref.Ref{}, pathError(path, "credential reference must use a supported locator scheme")
	}
	locator := ref.String()
	_, payload, found := strings.Cut(locator, ":")
	if found {
		if strings.TrimSpace(payload) == "" {
			return credentialref.Ref{}, pathError(path, "credential locator payload is required")
		}
	} else if !strings.HasPrefix(locator, "/") && !strings.HasPrefix(locator, "~/") {
		return credentialref.Ref{}, pathError(path, "credential locator must include its payload")
	}
	payload = strings.TrimSpace(payload)
	switch ref.Kind() {
	case credentialref.KindEnv:
		if !envCredentialNamePattern.MatchString(payload) {
			return credentialref.Ref{}, pathError(path, "environment credential name is invalid")
		}
	case credentialref.KindFile:
		filePath := locator
		if found {
			filePath = payload
		}
		if !filepath.IsAbs(filePath) && !strings.HasPrefix(filePath, "~/") {
			return credentialref.Ref{}, pathError(path, "credential file path must be absolute or home-relative")
		}
	case credentialref.KindSecret, credentialref.KindSecretFile:
		if !credentialNamePattern.MatchString(payload) {
			return credentialref.Ref{}, pathError(path, "credential name must contain URL-safe non-empty path segments")
		}
		for _, segment := range strings.Split(payload, "/") {
			if segment == "." || segment == ".." {
				return credentialref.Ref{}, pathError(path, "credential name cannot contain ambiguous path segments")
			}
		}
	}
	return ref, nil
}

var (
	envCredentialNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	credentialNamePattern    = regexp.MustCompile(`^[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*$`)
)

type OpenAIConnection struct{ credential credentialref.Ref }

func NewOpenAIConnection(raw string) (OpenAIConnection, error) {
	ref, err := parseCredential("connection.openai.credential", raw)
	return OpenAIConnection{ref}, err
}
func (OpenAIConnection) Provider() Provider              { return ProviderOpenAI }
func (OpenAIConnection) isConnection()                   {}
func (c OpenAIConnection) Credential() credentialref.Ref { return c.credential }

type AnthropicConnection struct{ credential credentialref.Ref }

func NewAnthropicConnection(raw string) (AnthropicConnection, error) {
	ref, err := parseCredential("connection.anthropic.credential", raw)
	return AnthropicConnection{ref}, err
}
func (AnthropicConnection) Provider() Provider              { return ProviderAnthropic }
func (AnthropicConnection) isConnection()                   {}
func (c AnthropicConnection) Credential() credentialref.Ref { return c.credential }

type OpenRouterConnection struct{ credential credentialref.Ref }

func NewOpenRouterConnection(raw string) (OpenRouterConnection, error) {
	ref, err := parseCredential("connection.openrouter.credential", raw)
	return OpenRouterConnection{ref}, err
}
func (OpenRouterConnection) Provider() Provider              { return ProviderOpenRouter }
func (OpenRouterConnection) isConnection()                   {}
func (c OpenRouterConnection) Credential() credentialref.Ref { return c.credential }

type ChatGPTConnection struct{ credential credentialref.Ref }

func NewChatGPTConnection(raw string) (ChatGPTConnection, error) {
	ref, err := parseCredential("connection.chatgpt.credential", raw)
	return ChatGPTConnection{ref}, err
}
func (ChatGPTConnection) Provider() Provider              { return ProviderChatGPT }
func (ChatGPTConnection) isConnection()                   {}
func (c ChatGPTConnection) Credential() credentialref.Ref { return c.credential }

type OllamaConnection struct{ baseURL URL }

func NewOllamaConnection(raw string) (OllamaConnection, error) {
	if strings.TrimSpace(raw) == "" {
		return OllamaConnection{}, nil
	}
	u, err := ParseURL("connection.ollama.base_url", raw)
	return OllamaConnection{u}, err
}
func (OllamaConnection) Provider() Provider     { return ProviderOllama }
func (OllamaConnection) isConnection()          {}
func (c OllamaConnection) BaseURL() (URL, bool) { return c.baseURL, c.baseURL.value != "" }

type AzureConnection struct {
	projectEndpoint URL
	credential      credentialref.Ref
}

func NewAzureConnection(endpoint, rawCredential string) (AzureConnection, error) {
	u, err := ParseURL("connection.azure.project_endpoint", endpoint)
	if err != nil {
		return AzureConnection{}, err
	}
	if !strings.Contains(u.String(), "/api/projects/") {
		return AzureConnection{}, pathError("connection.azure.project_endpoint", "must identify an Azure AI project endpoint")
	}
	ref, err := parseCredential("connection.azure.credential", rawCredential)
	if err != nil {
		return AzureConnection{}, err
	}
	return AzureConnection{projectEndpoint: u, credential: ref}, nil
}
func (AzureConnection) Provider() Provider              { return ProviderAzure }
func (AzureConnection) isConnection()                   {}
func (c AzureConnection) ProjectEndpoint() URL          { return c.projectEndpoint }
func (c AzureConnection) Credential() credentialref.Ref { return c.credential }

var bedrockRegionPattern = regexp.MustCompile(`^[a-z]{2}(?:-gov)?-[a-z]+-\d$`)

type BedrockRegion struct{ value string }

func ParseBedrockRegion(raw string) (BedrockRegion, error) {
	raw = strings.TrimSpace(raw)
	if !bedrockRegionPattern.MatchString(raw) {
		return BedrockRegion{}, pathError("connection.bedrock.region", "invalid AWS region")
	}
	return BedrockRegion{raw}, nil
}
func (r BedrockRegion) String() string { return r.value }

type BedrockConnection struct {
	region     BedrockRegion
	credential *credentialref.Ref
}

func NewBedrockConnection(region BedrockRegion, rawCredential string) (BedrockConnection, error) {
	if region.value == "" {
		return BedrockConnection{}, pathError("connection.bedrock.region", "region is required")
	}
	var ref *credentialref.Ref
	if strings.TrimSpace(rawCredential) != "" { // swobu:io-string source=boundary
		parsed, err := parseCredential("connection.bedrock.credential", rawCredential)
		if err != nil {
			return BedrockConnection{}, err
		}
		ref = &parsed
	}
	return BedrockConnection{region: region, credential: ref}, nil
}
func (BedrockConnection) Provider() Provider      { return ProviderBedrock }
func (BedrockConnection) isConnection()           {}
func (c BedrockConnection) Region() BedrockRegion { return c.region }
func (c BedrockConnection) Credential() credentialref.Ref {
	if c.credential == nil {
		return credentialref.Ref{}
	}
	return *c.credential
}

var headerNamePattern = regexp.MustCompile(`^[!#$%&'*+.^_` + "`" + `|~0-9A-Za-z-]+$`)

type CustomAuth interface{ isCustomAuth() }
type CustomHeaderAuth struct {
	name       string
	credential credentialref.Ref
}

func NewCustomHeaderAuth(name, raw string) (CustomHeaderAuth, error) {
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if !headerNamePattern.MatchString(name) {
		return CustomHeaderAuth{}, pathError("connection.custom.auth.header.name", "invalid HTTP header name")
	}
	ref, err := parseCredential("connection.custom.auth.header.credential", raw)
	if err != nil {
		return CustomHeaderAuth{}, err
	}
	return CustomHeaderAuth{name, ref}, nil
}
func (CustomHeaderAuth) isCustomAuth()                   {}
func (a CustomHeaderAuth) Name() string                  { return a.name }
func (a CustomHeaderAuth) Credential() credentialref.Ref { return a.credential }

type CustomConnection struct {
	baseURL URL
	auth    CustomAuth
}

func NewCustomConnection(baseURL string, auth CustomAuth) (CustomConnection, error) {
	u, err := ParseURL("connection.custom.base_url", baseURL)
	if err != nil {
		return CustomConnection{}, err
	}
	return CustomConnection{u, auth}, nil
}
func (CustomConnection) Provider() Provider { return ProviderCustom }
func (CustomConnection) isConnection()      {}
func (c CustomConnection) BaseURL() URL     { return c.baseURL }
func (c CustomConnection) Auth() CustomAuth { return c.auth }

func setConnectionCredential(connection Connection, raw string) (Connection, error) {
	switch c := connection.(type) {
	case OpenAIConnection:
		return NewOpenAIConnection(raw)
	case AnthropicConnection:
		return NewAnthropicConnection(raw)
	case OpenRouterConnection:
		return NewOpenRouterConnection(raw)
	case ChatGPTConnection:
		return NewChatGPTConnection(raw)
	case AzureConnection:
		ref, err := parseCredential("connection.azure.credential", raw)
		if err != nil {
			return nil, err
		}
		c.credential = ref
		return c, nil
	case BedrockConnection:
		return NewBedrockConnection(c.region, raw)
	case CustomConnection:
		header, ok := c.auth.(CustomHeaderAuth)
		if !ok {
			return nil, fmt.Errorf("%w: custom connection has no header auth", ErrCredentialUnsupported)
		}
		ref, err := parseCredential("connection.custom.auth.header.credential", raw)
		if err != nil {
			return nil, err
		}
		header.credential = ref
		c.auth = header
		return c, nil
	default:
		return nil, fmt.Errorf("%w: provider %q", ErrCredentialUnsupported, connection.Provider())
	}
}
