package profile

import (
	"fmt"
	"slices"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
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
	// AmbientLabel and ReferenceLabel are operator-facing authoring nouns for
	// AmbientOrReference. They never enter routing or provider requests.
	AmbientLabel   string
	ReferenceLabel string
}

// ModelDiscoveryMode states whether target authoring has an advisory model
// discovery facet. It is static authoring metadata, not a conclusion drawn
// from an empty probe result or a model name.
type ModelDiscoveryMode uint8

const (
	ModelDiscoveryModeInvalid ModelDiscoveryMode = iota
	ModelDiscoveryModeAdvisory
	ModelDiscoveryModeNone
)

const (
	ProviderSpecOllama      ProviderID = "ollama"
	ProviderSpecLMStudio    ProviderID = "lmstudio"
	ProviderSpecVLLM        ProviderID = "vllm"
	ProviderSpecOpenAI      ProviderID = "openai"
	ProviderSpecChatGPT     ProviderID = "chatgpt"
	ProviderSpecGemini      ProviderID = "gemini"
	ProviderSpecAnthropic   ProviderID = "anthropic"
	ProviderSpecDeepSeek    ProviderID = "deepseek"
	ProviderSpecKimi        ProviderID = "kimi"
	ProviderSpecMistral     ProviderID = "mistral"
	ProviderSpecCerebras    ProviderID = "cerebras"
	ProviderSpecWorkersAI   ProviderID = "workersai"
	ProviderSpecLLM7        ProviderID = "llm7"
	ProviderSpecRunPod      ProviderID = "runpod"
	ProviderSpecNVIDIA      ProviderID = "nvidia"
	ProviderSpecFriendli    ProviderID = "friendli"
	ProviderSpecTogether    ProviderID = "together"
	ProviderSpecDeepInfra   ProviderID = "deepinfra"
	ProviderSpecScaleway    ProviderID = "scaleway"
	ProviderSpecSambaNova   ProviderID = "sambanova"
	ProviderSpecStepFun     ProviderID = "stepfun"
	ProviderSpecNebius      ProviderID = "nebius"
	ProviderSpecGMI         ProviderID = "gmi"
	ProviderSpecGroq        ProviderID = "groq"
	ProviderSpecFireworks   ProviderID = "fireworks"
	ProviderSpecOpenRouter  ProviderID = "openrouter"
	ProviderSpecZAI         ProviderID = "zai"
	ProviderSpecBedrock     ProviderID = "bedrock"
	ProviderSpecAzure       ProviderID = "azure"
	ProviderSpecCustom      ProviderID = "custom"
	ProviderSpecNovita      ProviderID = "novita"
	ProviderSpecBaseten     ProviderID = "baseten"
	ProviderSpecHyperbolic  ProviderID = "hyperbolic"
	ProviderSpecSiliconFlow ProviderID = "siliconflow"
	ProviderSpecOVHCloud    ProviderID = "ovhcloud"
	ProviderSpecModelScope  ProviderID = "modelscope"
	ProviderSpecOpenCodeZen ProviderID = "opencode-zen"
	ProviderSpecNous        ProviderID = "nous"
	ProviderSpecCommandCode ProviderID = "commandcode"
	ProviderSpecVenice      ProviderID = "venice"
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
	ModelDiscovery      ModelDiscoveryMode
	CatalogItemLabel    string
	DefaultAuthHeader   string
	VisibleInOperatorUI bool
	// ProviderProtocols is ordered by preference. The first concrete provider
	// contract is the static default; operators may explicitly select any later
	// contract, including a delivery variant of the same semantic kind.
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

func ModelDiscoveryModeForSpec(spec string) ModelDiscoveryMode {
	provider, ok := profileFor(spec)
	if !ok {
		return ModelDiscoveryModeInvalid
	}
	return provider.ModelDiscovery
}

type ProviderProtocolSpec struct {
	// Name is the concrete provider contract persisted in routing and operator
	// projections. Kind names the shared semantic wire grammar; Delivery names
	// the upstream response carrier selected by this concrete contract.
	Name     string
	Kind     protocolkind.ProtocolKind
	Delivery delivery.Delivery
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
// routing, codecs, or operator projections. Concrete protocol names ending in
// `_stream` are SSE contracts; suffix-free names are buffered HTTP JSON
// contracts. Keeping that coherence in the catalog prevents a split-brain
// target from reaching Exchange.
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
	seenProtocols := make(map[string]struct{}, len(provider.ProviderProtocols))
	for _, protocol := range provider.ProviderProtocols {
		name := strings.TrimSpace(protocol.Name) // swobu:io-string source=domain
		if name == "" {
			return fmt.Errorf("provider %q has an invalid concrete protocol name", provider.ProviderID)
		}
		if _, duplicate := seenProtocols[name]; duplicate {
			return fmt.Errorf("provider %q duplicates concrete protocol %q", provider.ProviderID, name)
		}
		seenProtocols[name] = struct{}{}
		if protocol.Kind == "" || protocol.Kind.String() == "" {
			return fmt.Errorf("provider %q concrete protocol %q has no semantic kind", provider.ProviderID, name)
		}
		if err := protocol.Delivery.Validate(); err != nil {
			return fmt.Errorf("provider %q concrete protocol %q has invalid delivery: %w", provider.ProviderID, name, err)
		}
		if strings.HasSuffix(name, "_stream") {
			if !protocol.Delivery.IsStreaming() || protocol.Delivery.Framing != delivery.FramingSSE {
				return fmt.Errorf("provider %q streaming protocol %q must use SSE delivery", provider.ProviderID, name)
			}
		} else if protocol.Delivery != delivery.BufferedDelivery() {
			return fmt.Errorf("provider %q buffered protocol %q must use buffered delivery", provider.ProviderID, name)
		}
	}
	return nil
}

func validCredentialSpec(spec CredentialSpec) bool {
	ambientLabel := strings.TrimSpace(spec.AmbientLabel)
	referenceLabel := strings.TrimSpace(spec.ReferenceLabel)
	labelsAbsent := ambientLabel == "" && referenceLabel == ""
	switch spec.Requirement {
	case CredentialUnsupported:
		return spec.Authoring == CredentialAuthoringNone && labelsAbsent
	case CredentialRequired:
		return (spec.Authoring == CredentialAuthoringReference || spec.Authoring == CredentialAuthoringInteractive) && labelsAbsent
	case CredentialOptional:
		if spec.Authoring == CredentialAuthoringReference {
			return labelsAbsent
		}
		return spec.Authoring == CredentialAuthoringAmbientOrReference && ambientLabel != "" && referenceLabel != ""
	case CredentialRequiredOutsideLoopback:
		return spec.Authoring == CredentialAuthoringReference && labelsAbsent
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
