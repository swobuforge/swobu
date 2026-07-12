package profile

import (
	"slices"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type AuthKind string

const (
	AuthNone          AuthKind = "none"
	AuthCredentialRef AuthKind = "credential_ref"
)

// AuthMode names one provider auth path in the catalog.
//
// It is the canonical selector for credential-ref sources (env/file/keychain),
// ambient AWS auth, and interactive login/device flows. It is not a storage
// source type.
type AuthMode string

const (
	AuthModeEnv               AuthMode = "env"
	AuthModeFile              AuthMode = "file"
	AuthModeKeychain          AuthMode = "keychain"
	AuthModeAWSProfile        AuthMode = "aws_profile"
	AuthModeAWSEnvSession     AuthMode = "aws_env_session"
	AuthModeChatGPTLogin      AuthMode = "chatgpt_login"
	AuthModeChatGPTDeviceAuth AuthMode = "chatgpt_device_auth"
)

type AuthModeRequirement string

const (
	AuthModeRequirementAlways                AuthModeRequirement = "always"
	AuthModeRequirementNever                 AuthModeRequirement = "never"
	AuthModeRequirementExceptLoopbackExecute AuthModeRequirement = "except_loopback_execute_origin"
)

// AuthModeSpec declares one allowed auth path for a provider.
type AuthModeSpec struct {
	Mode        AuthMode
	Kind        AuthKind
	Requirement AuthModeRequirement
	Interactive bool
}

type Capability string

const (
	ProviderSpecOllama           ProviderID = "ollama"
	ProviderSpecOpenAI           ProviderID = "openai"
	ProviderSpecChatGPT          ProviderID = "chatgpt"
	ProviderSpecAnthropic        ProviderID = "anthropic"
	ProviderSpecOpenRouter       ProviderID = "openrouter"
	ProviderSpecBedrock          ProviderID = "bedrock"
	ProviderSpecAzure            ProviderID = "azure"
	ProviderSpecOpenAICompatible ProviderID = "openai_compatible"

	CapabilityModelCatalog Capability = "model_catalog"
	CapabilityStreaming    Capability = "streaming"
)

// Profile is one canonical provider declaration.
//
// Add/remove/evolve provider specs in this catalog only.
type Profile struct {
	ProviderID              ProviderID
	ProviderDisplayName     string
	SetupHint               string
	DefaultBaseURL          string
	DefaultCredentialEnvVar string
	DefaultAuthHeader       string
	VisibleInOperatorUI     bool
	ProviderProtocols       []ProviderProtocolSpec
	// AllowedAuthModes lists the auth paths this provider declares.
	AllowedAuthModes     []AuthModeSpec
	DeclaredCapabilities []Capability
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

func SupportsAuth(spec string, authKind AuthKind) bool {
	for _, mode := range AllowedAuthModesForSpec(spec) {
		supported := mode.Kind
		if supported == authKind {
			return true
		}
	}
	return false
}

func AllowedAuthModesForSpec(spec string) []AuthModeSpec {
	profile, ok := profileFor(spec)
	if !ok {
		return nil
	}
	return slices.Clone(profile.AllowedAuthModes)
}

func SupportedAuthModesForSpec(spec string) []AuthMode {
	modes := AllowedAuthModesForSpec(spec)
	out := make([]AuthMode, 0, len(modes))
	for _, mode := range modes {
		threadingMode := mode.Mode
		if strings.TrimSpace(string(threadingMode)) == "" { // swobu:io-string source=domain
			continue
		}
		out = append(out, threadingMode)
	}
	return slices.Compact(out)
}

func SupportsAuthMode(spec string, mode AuthMode) bool {
	for _, supported := range SupportedAuthModesForSpec(spec) {
		if supported == mode {
			return true
		}
	}
	return false
}

func IsInteractiveAuthMode(mode AuthMode) bool {
	switch mode {
	case AuthModeChatGPTLogin, AuthModeChatGPTDeviceAuth:
		return true
	default:
		return false
	}
}

func SupportsCapability(spec string, capability Capability) bool {
	profile, ok := profileFor(spec)
	if !ok {
		return false
	}
	for _, supported := range profile.DeclaredCapabilities {
		if supported == capability {
			return true
		}
	}
	return false
}

func DefaultExecuteBaseURL(spec string) string {
	profile, ok := profileFor(spec)
	if !ok {
		return ""
	}
	return profile.DefaultBaseURL
}

// DefaultEnvKeyForSpec returns the canonical environment variable name for a
// provider spec, or empty if the provider has no stable env key convention.
func DefaultEnvKeyForSpec(spec string) string {
	profile, ok := profileFor(spec)
	if !ok {
		return ""
	}
	return profile.DefaultCredentialEnvVar
}
