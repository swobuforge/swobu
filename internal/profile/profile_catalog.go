package profile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
)

// CredentialRequirement states whether a durable credential reference is a
// valid or required target fact. It does not describe where credentials live.
type CredentialRequirement uint8

const (
	CredentialUnsupported CredentialRequirement = iota
	CredentialRequired
	CredentialOptional
	CredentialRequiredOutsideLoopback
)

// CredentialAuthoring states how an operator supplies a credential that the
// connection requires. It is deliberately separate from CredentialRequirement:
// ChatGPT requires durable credential state but obtains it interactively,
// whereas Bedrock may use ambient AWS identity without a reference.
type CredentialAuthoring uint8

const (
	CredentialAuthoringInvalid CredentialAuthoring = iota
	CredentialAuthoringReference
	CredentialAuthoringNone
	CredentialAuthoringInteractive
	CredentialAuthoringAmbientOrReference
)

type CredentialSpec struct {
	Requirement     CredentialRequirement
	Authoring       CredentialAuthoring
	SuggestedEnvVar string
}

// ModelCatalogMode states whether target authoring can enumerate model
// identities. It is static authoring metadata, not a conclusion drawn from an
// empty probe result or a model name.
type ModelCatalogMode uint8

const (
	ModelCatalogModeInvalid ModelCatalogMode = iota
	ModelCatalogModeEnumerable
	ModelCatalogModeManual
)

const (
	ProviderSpecOllama     ProviderID = "ollama"
	ProviderSpecLMStudio   ProviderID = "lmstudio"
	ProviderSpecVLLM       ProviderID = "vllm"
	ProviderSpecOpenAI     ProviderID = "openai"
	ProviderSpecChatGPT    ProviderID = "chatgpt"
	ProviderSpecAnthropic  ProviderID = "anthropic"
	ProviderSpecDeepSeek   ProviderID = "deepseek"
	ProviderSpecKimi       ProviderID = "kimi"
	ProviderSpecFriendli   ProviderID = "friendli"
	ProviderSpecTogether   ProviderID = "together"
	ProviderSpecDeepInfra  ProviderID = "deepinfra"
	ProviderSpecScaleway   ProviderID = "scaleway"
	ProviderSpecSambaNova  ProviderID = "sambanova"
	ProviderSpecStepFun    ProviderID = "stepfun"
	ProviderSpecNebius     ProviderID = "nebius"
	ProviderSpecGMI        ProviderID = "gmi"
	ProviderSpecGroq       ProviderID = "groq"
	ProviderSpecFireworks  ProviderID = "fireworks"
	ProviderSpecOpenRouter ProviderID = "openrouter"
	ProviderSpecZAI        ProviderID = "zai"
	ProviderSpecBedrock    ProviderID = "bedrock"
	ProviderSpecAzure      ProviderID = "azure"
	ProviderSpecCustom     ProviderID = "custom"
)

type LocatorKind uint8

const (
	LocatorFixed LocatorKind = iota
	LocatorBaseURL
	LocatorAzureProject
	LocatorAWSRegion
)

// LocatorSpec declares the provider-specific connection fact authored by an
// operator. Fixed providers may carry a runtime default without exposing input.
type LocatorSpec struct {
	Kind    LocatorKind
	Label   string
	Default string
}

// Profile is one canonical provider declaration.
//
// Add/remove/evolve provider specs in this catalog only.
type Profile struct {
	ProviderID          ProviderID
	ProviderDisplayName string
	SetupHint           string
	// SetupKeywords are search/copy hints only. Locator owns connection
	// semantics; these keywords must not drive setup behavior.
	SetupKeywords       []string
	Locator             LocatorSpec
	Credential          CredentialSpec
	ConnectionShape     routing.ConnectionShape
	ModelCatalog        ModelCatalogMode
	CatalogItemLabel    string
	DefaultAuthHeader   string
	VisibleInOperatorUI bool
	ProtocolAuthoring   ProtocolAuthoring
	// ProviderProtocols is ordered by preference. The first concrete protocol
	// is the provider default; operators may explicitly select any later entry.
	ProviderProtocols []ProviderProtocolSpec
}

// ConnectionShapeForSpec returns the durable connection shape declared by the
// canonical provider catalog. Unknown provider identifiers have no shape.
func ConnectionShapeForSpec(spec string) (routing.ConnectionShape, bool) {
	provider, ok := profileFor(spec)
	if !ok {
		return 0, false
	}
	return provider.ConnectionShape, true
}

func ModelCatalogModeForSpec(spec string) ModelCatalogMode {
	provider, ok := profileFor(spec)
	if !ok {
		return ModelCatalogModeInvalid
	}
	return provider.ModelCatalog
}

type ProviderProtocolSpec struct {
	Name  string
	Kind  protocolkind.ProtocolKind
	Frame string
}

func profileFor(spec string) (Profile, bool) {
	normalizedSpec := strings.TrimSpace(spec) // swobu:io-string source=domain
	if normalizedSpec == "" {
		return Profile{}, false
	}
	for _, profile := range catalog() {
		if string(profile.ProviderID) == normalizedSpec {
			return profile, true
		}
	}
	return Profile{}, false
}

func All() []Profile {
	return slices.Clone(catalog())
}

// ProfileForSpec returns the canonical profile for one supported provider
// identifier. Consumers use its facts without copying catalog membership into
// their own lookup table.
func ProfileForSpec(spec string) (Profile, bool) {
	return profileFor(spec)
}

// ValidateCatalogProfile rejects incomplete profile metadata before it reaches
// routing, codecs, or operator projections. Newly introduced semantic facts
// deliberately have invalid zero values so a future profile cannot acquire
// plausible Standard/reference behavior by omission.
func ValidateCatalogProfile(provider Profile) error {
	if strings.TrimSpace(string(provider.ProviderID)) == "" {
		return fmt.Errorf("provider id is required")
	}
	switch provider.ConnectionShape {
	case routing.ConnectionShapeStandard, routing.ConnectionShapeZAI, routing.ConnectionShapeBedrock, routing.ConnectionShapeCustom:
	default:
		return fmt.Errorf("provider %q has an invalid connection shape", provider.ProviderID)
	}
	if !validCredentialSpec(provider.Credential) {
		return fmt.Errorf("provider %q has incompatible credential requirement and authoring mode", provider.ProviderID)
	}
	return nil
}

func validCredentialSpec(spec CredentialSpec) bool {
	switch spec.Requirement {
	case CredentialUnsupported:
		return spec.Authoring == CredentialAuthoringNone
	case CredentialRequired:
		return spec.Authoring == CredentialAuthoringReference || spec.Authoring == CredentialAuthoringInteractive
	case CredentialOptional:
		return spec.Authoring == CredentialAuthoringReference || spec.Authoring == CredentialAuthoringAmbientOrReference
	case CredentialRequiredOutsideLoopback:
		return spec.Authoring == CredentialAuthoringReference
	default:
		return false
	}
}

func SupportedSpecs() []string {
	entries := catalog()
	specs := make([]string, 0, len(entries))
	for _, entry := range entries {
		specs = append(specs, string(entry.ProviderID))
	}
	slices.Sort(specs)
	return specs
}

func SupportsSpec(spec string) bool {
	_, ok := profileFor(spec)
	return ok
}

func DefaultExecuteBaseURL(spec string) string {
	profile, ok := profileFor(spec)
	if !ok {
		return ""
	}
	return profile.Locator.Default
}

func LocatorSpecForProvider(spec string) (LocatorSpec, bool) {
	profile, ok := profileFor(spec)
	if !ok {
		return LocatorSpec{}, false
	}
	return profile.Locator, true
}

func RequiresLocator(spec string) bool {
	locator, ok := LocatorSpecForProvider(spec)
	if !ok {
		return false
	}
	return locator.Kind != LocatorFixed && strings.TrimSpace(locator.Default) == ""
}

// DefaultEnvKeyForSpec returns the canonical environment variable name for a
// provider spec, or empty if the provider has no stable env key convention.
func DefaultEnvKeyForSpec(spec string) string {
	profile, ok := profileFor(spec)
	if !ok {
		return ""
	}
	return profile.Credential.SuggestedEnvVar
}

func CatalogItemLabelForSpec(spec string) string {
	provider, ok := profileFor(spec)
	if !ok || strings.TrimSpace(provider.CatalogItemLabel) == "" {
		return "model"
	}
	return provider.CatalogItemLabel
}
