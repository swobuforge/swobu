package target_config

import (
	"strings"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
)

// catalog_projection.go owns pure live-catalog and protocol projections. Model
// inventory always comes through TargetSetupQueries; Cockpit ships no fallback
// snapshot that can silently drift from provider truth.

type protocolOption struct {
	ID       string
	Label    string
	Detail   string
	Keywords []string
}

// resolveProtocolOptions returns concrete protocol choices in product order.
// Provider/profile resolution owns protocol policy; Cockpit only converts the
// resolved list into picker options.
func resolveProtocolOptions(providerSpec string, model readmodel.ModelAuthoringOptionReadModel) []protocolOption {
	providerSpec = strings.TrimSpace(providerSpec) // swobu:io-string source=boundary
	resolution := profile.ResolveModelAuthoringOption(providerSpec, modelAuthoringOptionFromReadModel(model))
	return orderedProtocolOptions(providerSpec, resolution.ProtocolOptions())
}

func modelAuthoringOptionFromReadModel(model readmodel.ModelAuthoringOptionReadModel) profile.ModelAuthoringOption {
	name := strings.TrimSpace(model.Name) // swobu:io-string source=boundary
	if name == "" {
		name = strings.TrimSpace(model.ID) // swobu:io-string source=boundary
	}
	return profile.NewModelAuthoringOption(
		name,
		model.ModelName,
		model.ModelPublisher,
		model.ModelVersion,
		model.Family,
		model.SupportedProviderProtocols,
		model.DefaultProviderProtocol,
	)
}

func orderedProtocolOptions(providerSpec string, candidates []string) []protocolOption {
	allowed := map[string]struct{}{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate) // swobu:io-string source=boundary
		if candidate == "" {
			continue
		}
		canonical, err := profile.NormalizeProviderProtocolForSpec(providerSpec, candidate)
		if err != nil {
			continue
		}
		allowed[canonical] = struct{}{}
	}
	// The profile manifest is the concrete-contract authority. Model metadata
	// may narrow it, but it must not reorder delivery variants or collapse two
	// concrete contracts that share one semantic protocol kind.
	out := make([]protocolOption, 0, len(allowed))
	for _, protocol := range profile.ConcreteProviderProtocolsForSpec(providerSpec) {
		if _, ok := allowed[protocol]; !ok {
			continue
		}
		out = append(out, protocolOption{
			ID:       protocol,
			Label:    protocolOptionLabel(providerSpec, protocol),
			Keywords: protocolOptionKeywords(providerSpec, protocol),
		})
	}
	return out
}

func protocolOptionLabel(providerSpec, protocol string) string {
	providerName := strings.TrimSpace(providerSpec) // swobu:io-string source=boundary
	semanticName := protocol
	deliveryName := ""
	if spec, ok := profile.ProviderProtocolSpecForSpec(providerSpec, protocol); ok {
		providerName = protocolProviderLabel(spec.Kind.String())
		semanticName = protocolKindLabel(spec.Kind.String())
		deliveryName = spec.Delivery.Mode.String()
	}
	if deliveryName == "" {
		return providerName + " · " + semanticName
	}
	return providerName + " · " + semanticName + " · " + deliveryName
}

func protocolProviderLabel(protocol string) string {
	switch protocol {
	case "responses", "chat_completions":
		return "OpenAI"
	case "messages":
		return "Anthropic"
	case "interactions":
		return "Gemini"
	default:
		return protocol
	}
}

func protocolKindLabel(protocol string) string {
	switch protocol {
	case "responses":
		return "Responses"
	case "chat_completions":
		return "Chat Completions"
	case "messages":
		return "Messages"
	case "interactions":
		return "Interactions"
	default:
		return protocol
	}
}

func protocolOptionKeywords(providerSpec, protocol string) []string {
	label := protocolOptionLabel(providerSpec, protocol)
	return []string{
		protocol,
		strings.ReplaceAll(protocol, "_", " "),
		label,
		strings.ReplaceAll(label, "_", " "),
	}
}
