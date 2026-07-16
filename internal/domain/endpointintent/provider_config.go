package endpointintent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
)

type ProviderConfigRef struct {
	value string
}

// ParseProviderConfigRef validates the operator-visible provider-config
// reference as durable intent.
func ParseProviderConfigRef(raw string) (ProviderConfigRef, error) {
	if strings.TrimSpace(raw) == "" { // swobu:io-string source=domain
		return ProviderConfigRef{}, fmt.Errorf("%w: provider config ref must not be empty", ErrInvalidProviderConfigRef)
	}
	return ProviderConfigRef{value: raw}, nil
}

func (r ProviderConfigRef) String() string {
	return r.value
}

// NewOpaqueProviderConfigRef allocates one opaque provider-config identity for
// durable endpoint intent. Caller provides existing configs in one endpoint so
// allocation remains endpoint-local and collision-free.
func NewOpaqueProviderConfigRef(existing []ProviderConfig) (ProviderConfigRef, error) {
	used := make(map[string]struct{}, len(existing))
	for _, cfg := range existing {
		used[cfg.Ref().String()] = struct{}{}
	}
	randomRef := func() (string, error) {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		return hex.EncodeToString(b[:]), nil
	}
	for attempts := 0; attempts < 64; attempts++ {
		candidate, err := randomRef()
		if err != nil {
			return ProviderConfigRef{}, fmt.Errorf("%w: generate provider config ref: %v", ErrInvalidProviderConfigRef, err)
		}
		if _, exists := used[candidate]; exists {
			continue
		}
		used[candidate] = struct{}{}
		return ProviderConfigRef{value: candidate}, nil
	}
	return ProviderConfigRef{}, fmt.Errorf("%w: could not allocate unique provider config ref", ErrInvalidProviderConfigRef)
}

type ProviderSpec struct {
	value string
}

// ParseProviderSpec validates the durable provider-spec identifier used by one
// provider config.
func ParseProviderSpec(raw string) (ProviderSpec, error) {
	// swobu:io-string source=boundary from raw operator input to ProviderSpec
	spec := strings.ToLower(strings.TrimSpace(raw)) // swobu:io-string source=domain
	if spec == "" {
		return ProviderSpec{}, fmt.Errorf("%w: provider spec must not be empty", ErrInvalidProviderSpec)
	}
	if !profile.SupportsSpec(spec) {
		return ProviderSpec{}, fmt.Errorf(
			"%w: unsupported provider spec %q (supported: %s)",
			ErrInvalidProviderSpec,
			spec,
			strings.Join(profile.SupportedSpecs(), ", "),
		)
	}
	return ProviderSpec{value: spec}, nil
}

func (s ProviderSpec) String() string {
	return s.value
}

type ProviderConfig struct {
	ref              ProviderConfigRef
	providerSpec     ProviderSpec
	baseURL          string
	// credentialRef is a durable intent handle. Once bound, replay assumes it
	// is not repointed to a different provider account in place.
	credentialRef    string
	authHeader       string
	providerProtocol string
	// routeModelID is the client-visible route model name. Legacy configs may
	// leave it empty and fall back to modelID when projected.
	routeModelID     string
	modelID          string
	targetAlias      string
	targetRank       int
	targetWeight     int
}

var targetAliasPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

const primaryTargetSelector = "primary"

// NewProviderConfig validates the explicit provider-config declaration used by
// endpoint intent. It does not guess provider family or protocol semantics.
//
// client-family admissibility and provider wire realization are owned by
// request-path route rules and provider adapters. Durable endpoint
// intent stores provider identity and credentials, not transport dialect.
func NewProviderConfig(
	ref ProviderConfigRef,
	spec ProviderSpec,
	baseURL string,
	credentialRef string,
) (ProviderConfig, error) {
	if ref.value == "" {
		return ProviderConfig{}, fmt.Errorf("%w: provider config ref is required", ErrInvalidProviderConfig)
	}
	if spec.value == "" {
		return ProviderConfig{}, fmt.Errorf("%w: provider spec is required", ErrInvalidProviderConfig)
	}
	if spec.value == "openai_compatible" && strings.TrimSpace(baseURL) == "" { // swobu:io-string source=domain
		return ProviderConfig{}, fmt.Errorf("%w: OpenAI-style provider configs require a base URL", ErrInvalidProviderConfig)
	}
	normalizedBaseURL := strings.TrimSpace(baseURL) // swobu:io-string source=boundary
	if spec.value == "azure" && normalizedBaseURL != "" {
		normalizedLocator, err := NormalizeAzureResourceLocator(normalizedBaseURL)
		if err != nil {
			return ProviderConfig{}, fmt.Errorf("%w: %v", ErrInvalidProviderConfig, err)
		}
		normalizedBaseURL = normalizedLocator
	}
	providerProtocol := profile.ProviderProtocolAuto
	if spec.value != "azure" {
		concrete, ok := profile.ResolveConcreteProtocolForAutoAtBoundary(spec.value)
		if !ok {
			return ProviderConfig{}, fmt.Errorf("%w: provider spec has no default provider protocol", ErrInvalidProviderConfig)
		}
		providerProtocol = concrete
	}
	config := ProviderConfig{
		ref:              ref,
		providerSpec:     spec,
		baseURL:          normalizedBaseURL,
		credentialRef:    credentialRef,
		authHeader:       "",
		providerProtocol: providerProtocol,
		modelID:          "",
		targetAlias:      "",
		targetRank:       1,
		targetWeight:     1,
	}
	var err error
	config, err = config.WithAuthHeader("")
	if err != nil {
		return ProviderConfig{}, err
	}
	return config, nil
}

func (c ProviderConfig) Ref() ProviderConfigRef {
	return c.ref
}

func (c ProviderConfig) ProviderSpec() ProviderSpec {
	return c.providerSpec
}

func (c ProviderConfig) BaseURL() string {
	return c.baseURL
}

func (c ProviderConfig) CredentialRef() string {
	return c.credentialRef
}

func (c ProviderConfig) AuthHeader() string {
	header := strings.TrimSpace(c.authHeader) // swobu:io-string source=boundary
	if header != "" {
		return header
	}
	if c.providerSpec.String() == "openai_compatible" {
		return profile.DefaultAuthHeaderForSpec(c.providerSpec.String())
	}
	return ""
}

func isValidHTTPHeaderFieldName(name string) bool {
	if strings.TrimSpace(name) == "" { // swobu:io-string source=boundary
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return true
}

func (c ProviderConfig) ProtocolKind() protocolkind.ProtocolKind {
	kind, _, ok := profile.ProviderProtocolKindAndFrame(c.providerSpec.String(), c.providerProtocol)
	if ok {
		return kind
	}
	return ""
}

func (c ProviderConfig) ProviderProtocol() string {
	return c.providerProtocol
}

// WithProviderProtocolAuto preserves unresolved auto protocol selection so a
// save-boundary caller can hand protocol realization to the daemon resolver.
func (c ProviderConfig) WithProviderProtocolAuto() ProviderConfig {
	c.providerProtocol = profile.ProviderProtocolAuto
	return c
}

func (c ProviderConfig) WithProviderProtocol(providerProtocol string) (ProviderConfig, error) {
	providerProtocol = strings.TrimSpace(providerProtocol) // swobu:io-string source=domain
	if providerProtocol == "" {
		return ProviderConfig{}, fmt.Errorf("%w: provider protocol is required", ErrInvalidProviderConfig)
	}
	if !profile.SupportsProviderProtocolForSpec(c.providerSpec.String(), providerProtocol) {
		return ProviderConfig{}, fmt.Errorf(
			"%w: provider protocol %q is unsupported for provider %q",
			ErrInvalidProviderConfig,
			providerProtocol,
			c.providerSpec.String(),
		)
	}
	protocolKind, _, ok := profile.ProviderProtocolKindAndFrame(c.providerSpec.String(), providerProtocol)
	if !ok {
		return ProviderConfig{}, fmt.Errorf("%w: provider protocol %q has no protocol mapping", ErrInvalidProviderConfig, providerProtocol)
	}
	if !profile.SupportsExecutionProtocolForSpec(c.providerSpec.String(), protocolKind) {
		return ProviderConfig{}, fmt.Errorf(
			"%w: provider protocol %q protocol %q is unsupported for provider %q",
			ErrInvalidProviderConfig,
			providerProtocol,
			protocolKind,
			c.providerSpec.String(),
		)
	}
	c.providerProtocol = providerProtocol
	return c, nil
}

// WithAuthHeader stores the selected auth-header name for OpenAI-compatible
// provider configs.
func (c ProviderConfig) WithAuthHeader(authHeader string) (ProviderConfig, error) {
	authHeader = strings.TrimSpace(authHeader) // swobu:io-string source=domain
	if c.providerSpec.value == "" {
		return ProviderConfig{}, fmt.Errorf("%w: provider spec is required", ErrInvalidProviderConfig)
	}
	if c.providerSpec.String() != "openai_compatible" {
		if authHeader != "" {
			return ProviderConfig{}, fmt.Errorf("%w: auth header is unsupported for provider %q", ErrInvalidProviderConfig, c.providerSpec.String())
		}
		c.authHeader = ""
		return c, nil
	}
	if authHeader == "" {
		c.authHeader = profile.DefaultAuthHeaderForSpec(c.providerSpec.String())
		return c, nil
	}
	if !isValidHTTPHeaderFieldName(authHeader) {
		return ProviderConfig{}, fmt.Errorf("%w: auth header %q is invalid", ErrInvalidProviderConfig, authHeader)
	}
	c.authHeader = authHeader
	return c, nil
}

func (c ProviderConfig) SelectedFrame() string {
	_, frame, ok := profile.ProviderProtocolKindAndFrame(c.providerSpec.String(), c.providerProtocol)
	if ok {
		return frame
	}
	return ""
}

func (c ProviderConfig) ModelID() string {
	return c.modelID
}

// RouteModelID returns the client-visible route model name for this provider
// config. Explicit route_model_id wins; legacy configs fall back to modelID.
func (c ProviderConfig) RouteModelID() string {
	if routeModelID := strings.TrimSpace(c.routeModelID); routeModelID != "" { // swobu:io-string source=domain
		return routeModelID
	}
	return strings.TrimSpace(c.modelID) // swobu:io-string source=domain
}

// WithRouteModelID stores the client-visible route model name for this config.
// Empty is allowed so legacy configs can continue to load through the modelID
// fallback.
func (c ProviderConfig) WithRouteModelID(routeModelID string) (ProviderConfig, error) {
	c.routeModelID = strings.TrimSpace(routeModelID) // swobu:io-string source=domain
	return c, nil
}

func (c ProviderConfig) WithModelID(modelID string) (ProviderConfig, error) {
	c.modelID = strings.TrimSpace(modelID) // swobu:io-string source=domain
	return c, nil
}

func (c ProviderConfig) TargetAlias() string {
	return c.targetAlias
}

func (c ProviderConfig) WithTargetAlias(targetAlias string) (ProviderConfig, error) {
	targetAlias = strings.ToLower(strings.TrimSpace(targetAlias)) // swobu:io-string source=domain
	if targetAlias == "" {
		c.targetAlias = ""
		return c, nil
	}
	if targetAlias == primaryTargetSelector {
		return ProviderConfig{}, fmt.Errorf("%w: target alias %q is reserved", ErrInvalidProviderConfig, targetAlias)
	}
	if !targetAliasPattern.MatchString(targetAlias) {
		return ProviderConfig{}, fmt.Errorf(
			"%w: target alias %q must match %s",
			ErrInvalidProviderConfig,
			targetAlias,
			targetAliasPattern.String(),
		)
	}
	c.targetAlias = targetAlias
	return c, nil
}

func (c ProviderConfig) TargetRank() int {
	if c.targetRank <= 0 {
		return 1
	}
	return c.targetRank
}

func (c ProviderConfig) WithTargetRank(rank int) (ProviderConfig, error) {
	if rank < 1 {
		return ProviderConfig{}, fmt.Errorf("%w: target rank must be at least 1", ErrInvalidProviderConfig)
	}
	c.targetRank = rank
	return c, nil
}

func (c ProviderConfig) TargetWeight() int {
	if c.targetWeight <= 0 {
		return 1
	}
	return c.targetWeight
}

func (c ProviderConfig) WithTargetWeight(weight int) (ProviderConfig, error) {
	if weight < 1 {
		return ProviderConfig{}, fmt.Errorf("%w: target weight must be at least 1", ErrInvalidProviderConfig)
	}
	c.targetWeight = weight
	return c, nil
}

// projectedRouteModelID returns the route selector used by Cockpit and request
// routing. Route model names take precedence, then legacy model IDs.
func projectedRouteModelID(c ProviderConfig) string {
	return strings.TrimSpace(c.RouteModelID()) // swobu:io-string source=domain
}
