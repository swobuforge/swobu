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
		label := strings.TrimSpace(profile.SetupHint) // trimlowerlint:allow boundary canonicalization
		if label == "" {
			label = string(profile.ProviderID)
		}
		out = append(out, ProviderOption{Spec: string(profile.ProviderID), Label: label})
	}
	return out
}

func ProviderConfigForSpec(spec string, current ProviderConfigSnapshot) ProviderConfigSnapshot {
	spec = strings.TrimSpace(spec) // trimlowerlint:allow boundary canonicalization
	next := current
	if strings.TrimSpace(next.Ref) == "" { // trimlowerlint:allow boundary canonicalization
		next.Ref = DraftProviderRef
	}
	currentProtocol := protocolkind.ProtocolKind(strings.TrimSpace(next.ProtocolKind)) // trimlowerlint:allow boundary canonicalization
	if currentProtocol == "" || !supportsDraftProtocolForSpec(spec, currentProtocol) {
		next.ProtocolKind = defaultDraftProtocolForSpec(spec).String()
	}
	if frame, ok := providercatalog.DefaultFrameForSpecProtocol(spec, protocolkind.ProtocolKind(strings.TrimSpace(next.ProtocolKind))); ok {
		next.SelectedFrame = frame
	}
	next.ProviderSpec = spec
	defaultBaseURL := strings.TrimSpace(providercatalog.DefaultExecuteBaseURL(spec)) // trimlowerlint:allow boundary canonicalization
	if defaultBaseURL != "" {
		next.BaseURL = defaultBaseURL
	}
	// Provider switches must always force explicit credential re-selection.
	next.CredentialRef = ""
	return next
}

func ProviderRequiresCredential(spec, baseURL string) bool {
	return providercatalog.RequiresCredential(spec, baseURL)
}

func ProviderSupportsCatalog(spec string) bool {
	return providercatalog.SupportsCapability(spec, providercatalog.CapabilityModelCatalog)
}

func defaultDraftProtocolForSpec(spec string) protocolkind.ProtocolKind {
	if !providercatalog.SupportsSpec(spec) {
		return protocolkind.ChatCompletions
	}
	if strings.EqualFold(strings.TrimSpace(spec), "anthropic") { // trimlowerlint:allow boundary canonicalization
		return protocolkind.Messages
	}
	return protocolkind.ChatCompletions
}

func supportsDraftProtocolForSpec(spec string, protocol protocolkind.ProtocolKind) bool {
	if !providercatalog.SupportsSpec(spec) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(spec), "anthropic") { // trimlowerlint:allow boundary canonicalization
		return protocol == protocolkind.Messages
	}
	return protocol == protocolkind.ChatCompletions || protocol == protocolkind.Responses || protocol == protocolkind.Completions
}
