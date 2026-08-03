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
	case APIKeyConnection:
		right, ok := right.(APIKeyConnection)
		return ok && left.provider == right.provider && left.credential.String() == right.credential.String()
	case ZAIConnection:
		right, ok := right.(ZAIConnection)
		return ok && left.access == right.access && left.credential.String() == right.credential.String()
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

const DeepSeekProviderProtocol = "messages_stream"

// APIKeyConnection is the durable shape shared by fixed providers whose only
// authored connection fact is a credential reference.
type APIKeyConnection struct {
	provider   Provider
	credential credentialref.Ref
}

func NewAPIKeyConnection(provider Provider, raw string) (APIKeyConnection, error) {
	switch provider {
	case ProviderOpenAI, ProviderAnthropic, ProviderDeepSeek, ProviderOpenRouter, ProviderChatGPT:
	default:
		return APIKeyConnection{}, pathError("connection.provider", fmt.Sprintf("provider %q does not use an API-key connection", provider))
	}
	ref, err := parseCredential("connection."+string(provider)+".credential", raw)
	return APIKeyConnection{provider: provider, credential: ref}, err
}
func (c APIKeyConnection) Provider() Provider            { return c.provider }
func (APIKeyConnection) isConnection()                   {}
func (c APIKeyConnection) Credential() credentialref.Ref { return c.credential }

// ZAIAccess identifies the Z.AI commercial API surface selected by the
// operator. It is durable routing intent because it determines where requests
// execute and which account balance they consume.
type ZAIAccess string

const (
	ZAIAccessGeneralAPI ZAIAccess = "general_api"
	ZAIAccessCodingPlan ZAIAccess = "coding_plan"
	// ZAIProviderProtocol is the one execution protocol derived for every Z.AI
	// target. Access selects the commercial endpoint, never the wire family.
	ZAIProviderProtocol = "chat_completions_stream"
)

// ZAIAccesses returns the closed Z.AI access products for authoring.
func ZAIAccesses() []ZAIAccess {
	return []ZAIAccess{ZAIAccessGeneralAPI, ZAIAccessCodingPlan}
}

func ParseZAIAccess(raw string) (ZAIAccess, error) {
	access := ZAIAccess(strings.TrimSpace(raw))
	switch access {
	case ZAIAccessGeneralAPI, ZAIAccessCodingPlan:
		return access, nil
	default:
		return "", pathError("connection.zai.access", fmt.Sprintf("unsupported access %q", raw))
	}
}

// Label returns the operator-facing name of an access product.
func (a ZAIAccess) Label() string {
	switch a {
	case ZAIAccessGeneralAPI:
		return "General API"
	case ZAIAccessCodingPlan:
		return "Coding Plan"
	default:
		return ""
	}
}

type ZAIConnection struct {
	access     ZAIAccess
	credential credentialref.Ref
}

func NewZAIConnection(access ZAIAccess, raw string) (ZAIConnection, error) {
	normalized, err := ParseZAIAccess(string(access))
	if err != nil {
		return ZAIConnection{}, err
	}
	ref, err := parseCredential("connection.zai.credential", raw)
	if err != nil {
		return ZAIConnection{}, err
	}
	return ZAIConnection{access: normalized, credential: ref}, nil
}
func (ZAIConnection) Provider() Provider  { return ProviderZAI }
func (ZAIConnection) isConnection()       {}
func (c ZAIConnection) Access() ZAIAccess { return c.access }

// BaseURL returns the endpoint implied by the validated access product.
func (c ZAIConnection) BaseURL() string {
	switch c.access {
	case ZAIAccessGeneralAPI:
		return "https://api.z.ai/api/paas/v4"
	case ZAIAccessCodingPlan:
		return "https://api.z.ai/api/coding/paas/v4"
	default:
		return ""
	}
}
func (c ZAIConnection) Credential() credentialref.Ref { return c.credential }

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
	case APIKeyConnection:
		return NewAPIKeyConnection(c.provider, raw)
	case ZAIConnection:
		return NewZAIConnection(c.access, raw)
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
