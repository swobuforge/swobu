package routing

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// ConnectionShape is the closed durable routing sum. It deliberately models
// durable execution concepts, not catalog membership or runtime wire family.
type ConnectionShape uint8

const (
	ConnectionShapeInvalid ConnectionShape = iota
	ConnectionShapeStandard
	ConnectionShapeZAI
	ConnectionShapeBedrock
	ConnectionShapeCustom
)

// Connection is a sealed durable connection sum. Exactly one concrete shape
// is held by Target, so irrelevant field combinations are unrepresentable.
type Connection interface {
	Provider() Provider
	isConnection()
}

// connectionsEqual compares only durable provider configuration. Keeping this
// rule in the sealed sum prevents runtime-only fields from changing generation.
func connectionsEqual(left, right Connection) bool {
	switch left := left.(type) {
	case StandardConnection:
		right, ok := right.(StandardConnection)
		return ok && left.provider == right.provider && left.locator.String() == right.locator.String() && left.credential.String() == right.credential.String()
	case ZAIConnection:
		right, ok := right.(ZAIConnection)
		return ok && left.provider == right.provider && left.access == right.access && left.credential.String() == right.credential.String()
	case BedrockConnection:
		right, ok := right.(BedrockConnection)
		return ok && left.provider == right.provider && left.region == right.region && left.endpoint == right.endpoint && left.Credential().String() == right.Credential().String()
	case CustomConnection:
		right, ok := right.(CustomConnection)
		return ok && left.provider == right.provider && left.baseURL.String() == right.baseURL.String() && customAuthEqual(left.auth, right.auth)
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

const (
	DeepSeekProviderProtocol = "messages_stream"
	// KimiProviderProtocol is the sole Kimi execution protocol. Kimi's
	// compatibility dialect is handled at its outbound adapter, never in routing.
	KimiProviderProtocol = "chat_completions_stream"
)

// StandardConnection carries the common durable provider identity, optional
// locator, and optional credential reference. A profile owns the provider's
// fixed locator, credential policy, and locator grammar; runtime wire behavior
// remains outside routing.
type StandardConnection struct {
	provider   Provider
	locator    URL
	credential credentialref.Ref
}

// NewStandardConnection constructs an already catalog-validated Standard
// connection. Callers must obtain provider through ParseProvider at a profile
// construction edge rather than manufacture provider identifiers here.
func NewStandardConnection(provider Provider, rawLocator, rawCredential string) (StandardConnection, error) {
	connection := StandardConnection{provider: provider}
	path := "connection." + string(provider)
	if strings.TrimSpace(rawLocator) != "" {
		locator, err := ParseURL(path+".base_url", rawLocator)
		if err != nil {
			return StandardConnection{}, err
		}
		connection.locator = locator
	}
	if strings.TrimSpace(rawCredential) != "" {
		credential, err := parseCredential(path+".credential", rawCredential)
		if err != nil {
			return StandardConnection{}, err
		}
		connection.credential = credential
	}
	return connection, nil
}

func (c StandardConnection) Provider() Provider            { return c.provider }
func (StandardConnection) isConnection()                   {}
func (c StandardConnection) Locator() (URL, bool)          { return c.locator, c.locator.value != "" }
func (c StandardConnection) Credential() credentialref.Ref { return c.credential }

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
func ZAIAccesses() []ZAIAccess { return []ZAIAccess{ZAIAccessGeneralAPI, ZAIAccessCodingPlan} }

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

// ZAIConnection retains Z.AI's durable access-product selection.
type ZAIConnection struct {
	provider   Provider
	access     ZAIAccess
	credential credentialref.Ref
}

func NewZAIConnection(provider Provider, access ZAIAccess, raw string) (ZAIConnection, error) {
	normalized, err := ParseZAIAccess(string(access))
	if err != nil {
		return ZAIConnection{}, err
	}
	ref, err := parseCredential("connection."+string(provider)+".credential", raw)
	if err != nil {
		return ZAIConnection{}, err
	}
	return ZAIConnection{provider: provider, access: normalized, credential: ref}, nil
}

func (c ZAIConnection) Provider() Provider            { return c.provider }
func (ZAIConnection) isConnection()                   {}
func (c ZAIConnection) Access() ZAIAccess             { return c.access }
func (c ZAIConnection) Credential() credentialref.Ref { return c.credential }

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

// BedrockConnection owns the durable Bedrock transport identity: region (the
// SigV4 signing source), endpoint (the complete API base URL including its AWS
// namespace), and optional credential. Routing requires but does not normalize
// the operator-authored endpoint.
type BedrockConnection struct {
	provider   Provider
	region     BedrockRegion
	endpoint   string
	credential *credentialref.Ref
}

func NewBedrockConnection(provider Provider, region BedrockRegion, endpoint, rawCredential string) (BedrockConnection, error) {
	if region.value == "" {
		return BedrockConnection{}, pathError("connection.bedrock.region", "region is required")
	}
	endpoint = strings.TrimSpace(endpoint) // swobu:io-string source=boundary
	if endpoint == "" {
		return BedrockConnection{}, pathError("connection.bedrock.endpoint", "endpoint is required")
	}
	var ref *credentialref.Ref
	if strings.TrimSpace(rawCredential) != "" { // swobu:io-string source=boundary
		parsed, err := parseCredential("connection."+string(provider)+".credential", rawCredential)
		if err != nil {
			return BedrockConnection{}, err
		}
		ref = &parsed
	}
	return BedrockConnection{provider: provider, region: region, endpoint: endpoint, credential: ref}, nil
}

func (c BedrockConnection) Provider() Provider    { return c.provider }
func (BedrockConnection) isConnection()           {}
func (c BedrockConnection) Region() BedrockRegion { return c.region }
func (c BedrockConnection) Endpoint() string      { return c.endpoint }
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

// CustomConnection retains the user-selected auth-header behavior that is not
// represented by a standard bearer-style credential reference.
type CustomConnection struct {
	provider Provider
	baseURL  URL
	auth     CustomAuth
}

func NewCustomConnection(provider Provider, baseURL string, auth CustomAuth) (CustomConnection, error) {
	u, err := ParseURL("connection."+string(provider)+".base_url", baseURL)
	if err != nil {
		return CustomConnection{}, err
	}
	return CustomConnection{provider: provider, baseURL: u, auth: auth}, nil
}

func (c CustomConnection) Provider() Provider { return c.provider }
func (CustomConnection) isConnection()        {}
func (c CustomConnection) BaseURL() URL       { return c.baseURL }
func (c CustomConnection) Auth() CustomAuth   { return c.auth }
