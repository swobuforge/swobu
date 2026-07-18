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

var protocolPreference = []string{
	"responses",
	"responses_stream",
	"chat_completions",
	"chat_completions_stream",
	"messages",
	"messages_stream",
}

// resolveProtocolOptions returns concrete protocol choices in product order.
// Provider/profile resolution owns protocol policy; Cockpit only converts the
// resolved list into picker options.
func resolveProtocolOptions(providerSpec string, model readmodel.ModelDeploymentReadModel) []protocolOption {
	providerSpec = strings.TrimSpace(providerSpec) // swobu:io-string source=boundary
	resolution := profile.ResolveProviderDeployment(providerSpec, providerDeploymentRecordFromReadModel(model))
	return orderedProtocolOptions(providerSpec, resolution.ProtocolOptions())
}

func providerDeploymentRecordFromReadModel(model readmodel.ModelDeploymentReadModel) profile.ProviderDeploymentRecord {
	name := strings.TrimSpace(model.Name) // swobu:io-string source=boundary
	if name == "" {
		name = strings.TrimSpace(model.ID) // swobu:io-string source=boundary
	}
	return profile.NewProviderDeployment(
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
		if candidate == "" || candidate == profile.ProviderProtocolAuto {
			continue
		}
		if !profile.SupportsProviderProtocolForSpec(providerSpec, candidate) {
			continue
		}
		allowed[candidate] = struct{}{}
	}
	out := make([]protocolOption, 0, len(allowed))
	for _, protocol := range protocolPreference {
		if _, ok := allowed[protocol]; !ok {
			continue
		}
		out = append(out, protocolOption{
			ID:       protocol,
			Label:    protocolOptionLabel(protocol),
			Keywords: protocolOptionKeywords(protocol),
		})
		delete(allowed, protocol)
	}
	for _, protocol := range profile.SupportedProviderProtocolsForSpec(providerSpec) {
		if _, ok := allowed[protocol]; !ok {
			continue
		}
		out = append(out, protocolOption{
			ID:       protocol,
			Label:    protocolOptionLabel(protocol),
			Keywords: protocolOptionKeywords(protocol),
		})
		delete(allowed, protocol)
	}
	return out
}

func protocolOptionLabel(protocol string) string {
	switch protocol {
	case "responses_stream":
		return "OpenAI · Responses · stream"
	case "responses":
		return "OpenAI · Responses"
	case "chat_completions_stream":
		return "OpenAI · Chat Completions · stream"
	case "chat_completions":
		return "OpenAI · Chat Completions"
	case "messages_stream":
		return "Anthropic · Messages · stream"
	case "messages":
		return "Anthropic · Messages"
	default:
		return protocol
	}
}

func protocolOptionKeywords(protocol string) []string {
	label := protocolOptionLabel(protocol)
	return []string{
		protocol,
		strings.ReplaceAll(protocol, "_", " "),
		label,
		strings.ReplaceAll(label, "_", " "),
	}
}
