package profile

import (
	"slices"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
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

type CredentialSpec struct {
	Requirement     CredentialRequirement
	SuggestedEnvVar string
}

const (
	ProviderSpecOllama     ProviderID = "ollama"
	ProviderSpecOpenAI     ProviderID = "openai"
	ProviderSpecChatGPT    ProviderID = "chatgpt"
	ProviderSpecAnthropic  ProviderID = "anthropic"
	ProviderSpecDeepSeek   ProviderID = "deepseek"
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
	CatalogItemLabel    string
	DefaultAuthHeader   string
	VisibleInOperatorUI bool
	ProtocolAuthoring   ProtocolAuthoring
	ProviderProtocols   []ProviderProtocolSpec
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
