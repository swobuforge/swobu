package endpointintent

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

type ProviderConfigRef struct {
	value string
}

// ParseProviderConfigRef validates the operator-visible provider-config
// reference as durable intent.
func ParseProviderConfigRef(raw string) (ProviderConfigRef, error) {
	if strings.TrimSpace(raw) == "" {
		return ProviderConfigRef{}, fmt.Errorf("%w: provider config ref must not be empty", ErrInvalidProviderConfigRef)
	}
	return ProviderConfigRef{value: raw}, nil
}

func (r ProviderConfigRef) String() string {
	return r.value
}

type ProviderSpec struct {
	value string
}

// ParseProviderSpec validates the durable provider-spec identifier used by one
// provider config.
func ParseProviderSpec(raw string) (ProviderSpec, error) {
	spec := strings.TrimSpace(raw)
	if spec == "" {
		return ProviderSpec{}, fmt.Errorf("%w: provider spec must not be empty", ErrInvalidProviderSpec)
	}
	if !providercatalog.SupportsSpec(spec) {
		return ProviderSpec{}, fmt.Errorf(
			"%w: unsupported provider spec %q (supported: %s)",
			ErrInvalidProviderSpec,
			spec,
			strings.Join(providercatalog.SupportedSpecs(), ", "),
		)
	}
	return ProviderSpec{value: spec}, nil
}

func (s ProviderSpec) String() string {
	return s.value
}

type ProviderConfig struct {
	ref           ProviderConfigRef
	providerSpec  ProviderSpec
	baseURL       string
	credentialRef string
	modelID       string
	targetAlias   string
	protocolKind  protocolsurface.Kind
}

var targetAliasPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// NewProviderConfig validates the explicit provider-config declaration used by
// endpoint intent. It does not guess provider family or protocol semantics.
//
// protocolKind here is the selected provider-side egress wire family for this
// target (for example `responses` vs `chat_completions`). It is not a claim
// about which client ingress family can be accepted at request time.
//
// Ingress-family admissibility is owned by request-path compatibility rules;
// this constructor owns durable target-route validity.
func NewProviderConfig(
	ref ProviderConfigRef,
	spec ProviderSpec,
	baseURL string,
	credentialRef string,
	protocolKind protocolsurface.Kind,
) (ProviderConfig, error) {
	if ref.value == "" {
		return ProviderConfig{}, fmt.Errorf("%w: provider config ref is required", ErrInvalidProviderConfig)
	}
	if spec.value == "" {
		return ProviderConfig{}, fmt.Errorf("%w: provider spec is required", ErrInvalidProviderConfig)
	}
	if strings.TrimSpace(protocolKind.String()) == "" {
		return ProviderConfig{}, fmt.Errorf("%w: protocol kind must not be empty", ErrInvalidProviderConfig)
	}
	if spec.value == "custom" && strings.TrimSpace(baseURL) == "" {
		return ProviderConfig{}, fmt.Errorf("%w: custom provider configs require a base URL", ErrInvalidProviderConfig)
	}
	if !providercatalog.SupportsRoute(spec.value, protocolKind) {
		return ProviderConfig{}, fmt.Errorf(
			"%w: unsupported provider route %q + %q",
			ErrInvalidProviderConfig,
			spec.value,
			protocolKind,
		)
	}
	return ProviderConfig{
		ref:           ref,
		providerSpec:  spec,
		baseURL:       baseURL,
		credentialRef: credentialRef,
		modelID:       "",
		targetAlias:   "",
		protocolKind:  protocolKind,
	}, nil
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

func (c ProviderConfig) ModelID() string {
	return c.modelID
}

func (c ProviderConfig) WithModelID(modelID string) (ProviderConfig, error) {
	c.modelID = strings.TrimSpace(modelID)
	return c, nil
}

func (c ProviderConfig) TargetAlias() string {
	return c.targetAlias
}

func (c ProviderConfig) WithTargetAlias(targetAlias string) (ProviderConfig, error) {
	targetAlias = strings.ToLower(strings.TrimSpace(targetAlias))
	if targetAlias == "" {
		c.targetAlias = ""
		return c, nil
	}
	if targetAlias == compatibility.PrimaryTargetSelector {
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

func (c ProviderConfig) ProtocolKind() protocolsurface.Kind {
	return c.protocolKind
}
