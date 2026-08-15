package profile

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

var (
	providerProtocolsNovita = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsBaseten = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		streamingProtocol("messages_stream", protocolkind.Messages),
	}
	providerProtocolsHyperbolic = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
	}
	providerProtocolsSiliconFlow = []ProviderProtocolSpec{
		streamingProtocol("chat_completions_stream", protocolkind.ChatCompletions),
		streamingProtocol("messages_stream", protocolkind.Messages),
	}
)

func bufferedProtocol(name string, kind protocolkind.ProtocolKind) ProviderProtocolSpec {
	return ProviderProtocolSpec{Name: name, Kind: kind, Delivery: delivery.BufferedDelivery()}
}

func streamingProtocol(name string, kind protocolkind.ProtocolKind) ProviderProtocolSpec {
	return ProviderProtocolSpec{Name: name, Kind: kind, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}
}

// ConcreteProviderProtocolsForSpec returns concrete provider contracts in the
// catalog-declared order. Buffered and streaming entries with the same Kind
// remain distinct because they select different upstream wire contracts.
func ConcreteProviderProtocolsForSpec(spec string) []string {
	profile, ok := profileFor(spec)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(profile.ProviderProtocols))
	for _, protocol := range profile.ProviderProtocols {
		name := strings.TrimSpace(protocol.Name) // swobu:io-string source=domain
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

// DerivedProtocolForSpec returns the catalog-selected protocol that operator
// authoring omits. Routing remains the authority that validates and
// materializes the derived protocol.
func DerivedProtocolForSpec(spec string) (string, bool) {
	provider, ok := profileFor(spec)
	if !ok {
		return "", false
	}
	return derivedProtocolForProfile(provider)
}

func derivedProtocolForProfile(provider Profile) (string, bool) {
	concrete := concreteProviderProtocols(provider)
	if len(concrete) != 1 {
		return "", false
	}
	return concrete[0].Name, true
}

func concreteProviderProtocols(provider Profile) []ProviderProtocolSpec {
	concrete := make([]ProviderProtocolSpec, 0, len(provider.ProviderProtocols))
	for _, protocol := range provider.ProviderProtocols {
		if strings.TrimSpace(protocol.Name) == "" {
			continue
		}
		concrete = append(concrete, protocol)
	}
	return concrete
}

func SupportsProviderProtocolForSpec(spec string, providerProtocol string) bool {
	normalized := strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	if normalized == "" {
		return false
	}
	_, err := NormalizeProviderProtocolForSpec(spec, normalized)
	return err == nil
}

// ProviderProtocolSpecForSpec resolves one exact concrete provider contract.
func ProviderProtocolSpecForSpec(spec string, providerProtocol string) (ProviderProtocolSpec, bool) {
	normalized, err := NormalizeProviderProtocolForSpec(spec, providerProtocol)
	if err != nil {
		return ProviderProtocolSpec{}, false
	}
	entry, ok := profileFor(spec)
	if !ok {
		return ProviderProtocolSpec{}, false
	}
	for _, supported := range entry.ProviderProtocols {
		if supported.Name == normalized {
			return supported, true
		}
	}
	return ProviderProtocolSpec{}, false
}

// ProviderProtocolKind resolves one exact concrete provider protocol to its
// shared semantic wire family.
func ProviderProtocolKind(spec string, providerProtocol string) (protocolkind.ProtocolKind, bool) {
	protocol, ok := ProviderProtocolSpecForSpec(spec, providerProtocol)
	if !ok {
		return "", false
	}
	return protocol.Kind, true
}

// NormalizeProviderProtocolForSpec validates one authored or persisted exact
// concrete protocol token. Delivery-bearing suffixes are not migration
// spellings: they are part of the selected provider contract.
func NormalizeProviderProtocolForSpec(spec string, providerProtocol string) (string, error) {
	normalized := strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	if normalized == "" {
		return "", nil
	}
	if !supportsCanonicalProviderProtocol(spec, normalized) {
		return "", fmt.Errorf("provider protocol %q is unsupported for provider %q", providerProtocol, spec)
	}
	return normalized, nil
}

func supportsCanonicalProviderProtocol(spec string, providerProtocol string) bool {
	entry, ok := profileFor(spec)
	if !ok {
		return false
	}
	for _, supported := range entry.ProviderProtocols {
		if supported.Name == providerProtocol {
			return true
		}
	}
	return false
}

func EncodeProviderProtocolForPersistence(providerProtocol string) string {
	normalized := strings.TrimSpace(providerProtocol) // swobu:io-string source=domain
	if normalized == "" {
		return ""
	}
	return normalized
}

// DecodeProviderProtocolFromPersistence normalizes one persisted provider
// protocol token. Empty means "unspecified" and is valid.
func DecodeProviderProtocolFromPersistence(spec string, providerProtocol string) (string, error) {
	normalized, err := NormalizeProviderProtocolForSpec(spec, providerProtocol)
	if err != nil {
		return "", fmt.Errorf("provider protocol %q is invalid for provider %q: %w", providerProtocol, spec, err)
	}
	return normalized, nil
}
