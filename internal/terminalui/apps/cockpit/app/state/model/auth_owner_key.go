package model

import "strings"

type AuthOwnerKey string

const (
	AuthOwnerPrefixEndpointProvider AuthOwnerKey = "endpoint_provider"
	AuthOwnerPrefixAddModelDraft    AuthOwnerKey = "add_model_draft"
	AuthOwnerPrefixCreateDraft      AuthOwnerKey = "create_draft"
)

func EndpointProviderAuthOwnerKey(endpointName string, providerRef string) AuthOwnerKey {
	return AuthOwnerPrefixEndpointProvider.compose(endpointName, providerRef)
}

func AddModelDraftAuthOwnerKey(endpointName string, draftRef string) AuthOwnerKey {
	return AuthOwnerPrefixAddModelDraft.compose(endpointName, draftRef)
}

func CreateDraftAuthOwnerKey(draftRef string) AuthOwnerKey {
	return AuthOwnerPrefixCreateDraft.compose("", draftRef)
}

func (k AuthOwnerKey) String() string {
	return strings.TrimSpace(string(k)) // trimlowerlint:allow boundary canonicalization
}

func (k AuthOwnerKey) Prefix() string {
	owner := k.String()
	if owner == "" {
		return ""
	}
	parts := strings.SplitN(owner, "|", 2)
	return strings.TrimSpace(parts[0]) // trimlowerlint:allow boundary canonicalization
}

func (k AuthOwnerKey) IsAddModelDraft() bool {
	return k.Prefix() == AuthOwnerPrefixAddModelDraft.String()
}

func (k AuthOwnerKey) IsEndpointProvider() bool {
	return k.Prefix() == AuthOwnerPrefixEndpointProvider.String()
}

func (k AuthOwnerKey) IsCreateDraft() bool {
	return k.Prefix() == AuthOwnerPrefixCreateDraft.String()
}

func (k AuthOwnerKey) EndpointName() string {
	_, endpointName, _ := k.parts()
	return endpointName
}

func (k AuthOwnerKey) ProviderRef() string {
	_, _, providerRef := k.parts()
	return providerRef
}

func (k AuthOwnerKey) compose(endpointName string, providerRef string) AuthOwnerKey {
	prefix := strings.TrimSpace(string(k))         // trimlowerlint:allow boundary canonicalization
	endpointName = strings.TrimSpace(endpointName) // trimlowerlint:allow boundary canonicalization
	providerRef = strings.TrimSpace(providerRef)   // trimlowerlint:allow boundary canonicalization
	return AuthOwnerKey(prefix + "|" + endpointName + "|" + providerRef)
}

func (k AuthOwnerKey) parts() (prefix string, endpointName string, providerRef string) {
	raw := k.String()
	if raw == "" {
		return "", "", ""
	}
	parts := strings.SplitN(raw, "|", 3)
	if len(parts) > 0 {
		prefix = strings.TrimSpace(parts[0]) // trimlowerlint:allow boundary canonicalization
	}
	if len(parts) > 1 {
		endpointName = strings.TrimSpace(parts[1]) // trimlowerlint:allow boundary canonicalization
	}
	if len(parts) > 2 {
		providerRef = strings.TrimSpace(parts[2]) // trimlowerlint:allow boundary canonicalization
	}
	return prefix, endpointName, providerRef
}
