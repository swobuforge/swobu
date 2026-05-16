package endpointintent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

type ProviderConfigRef struct {
	value string
}

// ParseProviderConfigRef validates the operator-visible provider-config
// reference as durable intent.
func ParseProviderConfigRef(raw string) (ProviderConfigRef, error) {
	if strings.TrimSpace(raw) == "" { // trimlowerlint:allow domain canonicalization
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
	// trimlowerlint:allow boundary canonicalization from raw operator input to ProviderSpec
	spec := strings.ToLower(strings.TrimSpace(raw)) // trimlowerlint:allow domain canonicalization
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
	protocolKind  protocolkind.ProtocolKind
	selectedFrame string
	modelID       string
	targetAlias   string
}

var targetAliasPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

const primaryTargetSelector = "primary"

// NewProviderConfig validates the explicit provider-config declaration used by
// endpoint intent. It does not guess provider family or protocol semantics.
//
// Ingress-family admissibility and provider wire realization are owned by
// request-path compatibility rules and provider adapters. Durable endpoint
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
	if spec.value == "openai_compatible" && strings.TrimSpace(baseURL) == "" { // trimlowerlint:allow domain canonicalization
		return ProviderConfig{}, fmt.Errorf("%w: OpenAI-compatible provider configs require a base URL", ErrInvalidProviderConfig)
	}
	protocolKind, ok := providercatalog.DefaultExecutionProtocolForSpec(spec.value)
	if !ok {
		return ProviderConfig{}, fmt.Errorf("%w: provider spec has no supported execution protocols", ErrInvalidProviderConfig)
	}
	selectedFrame, ok := providercatalog.DefaultFrameForSpecProtocol(spec.value, protocolKind)
	if !ok {
		return ProviderConfig{}, fmt.Errorf("%w: provider protocol has no supported execution frames", ErrInvalidProviderConfig)
	}
	return ProviderConfig{
		ref:           ref,
		providerSpec:  spec,
		baseURL:       baseURL,
		credentialRef: credentialRef,
		protocolKind:  protocolKind,
		selectedFrame: selectedFrame,
		modelID:       "",
		targetAlias:   "",
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

func (c ProviderConfig) ProtocolKind() protocolkind.ProtocolKind {
	return c.protocolKind
}

func (c ProviderConfig) WithProtocolKind(protocolKind protocolkind.ProtocolKind) (ProviderConfig, error) {
	if !providercatalog.SupportsExecutionProtocolForSpec(c.providerSpec.String(), protocolKind) {
		return ProviderConfig{}, fmt.Errorf(
			"%w: protocol %q is unsupported for provider %q",
			ErrInvalidProviderConfig,
			protocolKind,
			c.providerSpec.String(),
		)
	}
	selectedFrame, ok := providercatalog.DefaultFrameForSpecProtocol(c.providerSpec.String(), protocolKind)
	if !ok {
		return ProviderConfig{}, fmt.Errorf(
			"%w: provider %q protocol %q has no supported execution frames",
			ErrInvalidProviderConfig,
			c.providerSpec.String(),
			protocolKind,
		)
	}
	c.protocolKind = protocolKind
	c.selectedFrame = selectedFrame
	return c, nil
}

func (c ProviderConfig) SelectedFrame() string {
	return c.selectedFrame
}

func (c ProviderConfig) WithSelectedFrame(selectedFrame string) (ProviderConfig, error) {
	selectedFrame = strings.TrimSpace(selectedFrame) // trimlowerlint:allow domain canonicalization
	if selectedFrame == "" {
		return ProviderConfig{}, fmt.Errorf("%w: selected frame is required", ErrInvalidProviderConfig)
	}
	if !providercatalog.SupportsFrameForSpecProtocol(c.providerSpec.String(), c.protocolKind, selectedFrame) {
		return ProviderConfig{}, fmt.Errorf(
			"%w: selected frame %q is unsupported for provider %q protocol %q",
			ErrInvalidProviderConfig,
			selectedFrame,
			c.providerSpec.String(),
			c.protocolKind,
		)
	}
	c.selectedFrame = selectedFrame
	return c, nil
}

func (c ProviderConfig) ModelID() string {
	return c.modelID
}

func (c ProviderConfig) WithModelID(modelID string) (ProviderConfig, error) {
	c.modelID = strings.TrimSpace(modelID) // trimlowerlint:allow domain canonicalization
	return c, nil
}

func (c ProviderConfig) TargetAlias() string {
	return c.targetAlias
}

func (c ProviderConfig) WithTargetAlias(targetAlias string) (ProviderConfig, error) {
	targetAlias = strings.ToLower(strings.TrimSpace(targetAlias)) // trimlowerlint:allow domain canonicalization
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
