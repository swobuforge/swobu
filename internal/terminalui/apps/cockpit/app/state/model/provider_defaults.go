package model

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

const DraftProviderRef = compatibility.PrimaryTargetSelector

type ProviderOption struct {
	Spec  string
	Label string
}

func ProviderOptions() []ProviderOption {
	out := make([]ProviderOption, 0, len(providercatalog.All()))
	for _, profile := range providercatalog.All() {
		if !profile.OperatorVisible {
			continue
		}
		label := strings.TrimSpace(profile.OperatorSetupLabel)
		if label == "" {
			label = strings.TrimSpace(profile.Spec)
		}
		out = append(out, ProviderOption{Spec: profile.Spec, Label: label})
	}
	return out
}

func ProviderConfigForSpec(spec string, current ProviderConfigSnapshot) ProviderConfigSnapshot {
	spec = strings.TrimSpace(spec)
	next := current
	if strings.TrimSpace(next.Ref) == "" {
		next.Ref = DraftProviderRef
	}
	currentProtocol := protocolsurface.Kind(strings.TrimSpace(next.ProtocolKind))
	if currentProtocol == "" || !providercatalog.SupportsRoute(spec, currentProtocol) {
		defaultProtocol, ok := providercatalog.DefaultProtocolForSpec(spec)
		if ok {
			next.ProtocolKind = defaultProtocol.String()
		} else {
			next.ProtocolKind = ""
		}
	}
	next.ProviderSpec = spec
	defaultBaseURL := strings.TrimSpace(providercatalog.DefaultBaseURL(spec))
	if defaultBaseURL != "" {
		next.BaseURL = defaultBaseURL
	}
	if strings.TrimSpace(spec) == "ollama" {
		next.CredentialRef = ""
	}
	return next
}

func ProviderRequiresCredential(spec, baseURL string) bool {
	return providercatalog.RequiresCredential(spec, baseURL)
}

func ProviderSupportsCatalog(spec string) bool {
	return providercatalog.SupportsModelCatalog(spec)
}
