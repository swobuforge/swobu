package model

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

const DraftProviderRef = canonical.PrimaryTargetSelector

type ProviderOption struct {
	Spec  string
	Label string
}

func ProviderOptions() []ProviderOption {
	out := make([]ProviderOption, 0, len(providercatalog.All()))
	for _, profile := range providercatalog.All() {
		if !profile.VisibleInOperatorUI {
			continue
		}
		label := trimModelInput(profile.SetupHint)
		if label == "" {
			label = string(profile.ProviderID)
		}
		out = append(out, ProviderOption{Spec: string(profile.ProviderID), Label: label})
	}
	return out
}

func ProviderConfigForSpec(spec string, current ProviderConfigSnapshot) ProviderConfigSnapshot {
	spec = trimModelInput(spec)
	next := current
	if trimModelInput(next.Ref) == "" {
		next.Ref = DraftProviderRef
	}
	currentProtocol := protocolkind.ProtocolKind(trimModelInput(next.ProtocolKind))
	if currentProtocol == "" || !supportsDraftProtocolForSpec(spec, currentProtocol) {
		next.ProtocolKind = defaultDraftProtocolForSpec(spec).String()
	}
	if frame, ok := providercatalog.DefaultFrameForSpecProtocol(spec, protocolkind.ProtocolKind(trimModelInput(next.ProtocolKind))); ok {
		next.SelectedFrame = frame
	}
	next.ProviderSpec = spec
	defaultBaseURL := trimModelInput(providercatalog.DefaultExecuteBaseURL(spec))
	if defaultBaseURL != "" {
		next.BaseURL = defaultBaseURL
	}
	if !strings.EqualFold(spec, "bedrock") {
		next.Region = ""
	}
	// Provider switches must always force explicit credential re-selection.
	next.CredentialRef = ""
	return next
}

func ProviderRequiresCredential(spec, baseURL string) bool {
	spec = trimModelInput(spec)
	baseURL = trimModelInput(baseURL)
	return providercatalog.RequiresCredential(spec, baseURL)
}

func ProviderSupportsCatalog(spec string) bool {
	return providercatalog.SupportsCapability(spec, providercatalog.CapabilityModelCatalog)
}

func defaultDraftProtocolForSpec(spec string) protocolkind.ProtocolKind {
	if !providercatalog.SupportsSpec(spec) {
		return protocolkind.ChatCompletions
	}
	if strings.EqualFold(trimModelInput(spec), "anthropic") {
		return protocolkind.Messages
	}
	return protocolkind.ChatCompletions
}

func supportsDraftProtocolForSpec(spec string, protocol protocolkind.ProtocolKind) bool {
	if !providercatalog.SupportsSpec(spec) {
		return false
	}
	if strings.EqualFold(trimModelInput(spec), "anthropic") {
		return protocol == protocolkind.Messages
	}
	return protocol == protocolkind.ChatCompletions || protocol == protocolkind.Responses || protocol == protocolkind.Completions
}

func trimModelInput(value string) string {
	return strings.TrimSpace(value) // swobu:io-string source=boundary
}
