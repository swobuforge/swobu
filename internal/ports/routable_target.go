package ports

import "github.com/swobuforge/swobu/internal/domain/protocolkind"

type RoutableTarget struct {
	BackendRef string
	// ProviderSpec is the canonical provider family identity.
	ProviderSpec  string
	BaseURL       string
	CredentialRef string
	// ProtocolKind is the concrete selected provider-side egress protocol
	// surface, not a vendor umbrella and not a client-ingress selector.
	// Client ingress is normalized to canonical operations before provider
	// encoding.
	ProtocolKind     protocolkind.ProtocolKind
	AuthKind         string
	SelectedFrame    string
	ProviderProtocol string
}

func NewRoutableTarget(
	backendRef string,
	providerSpec string,
	baseURL string,
	credentialRef string,
	protocolKind protocolkind.ProtocolKind,
	authKind string,
	selectedFrame string,
	providerProtocol ...string,
) RoutableTarget {
	resolvedProviderProtocol := ""
	if len(providerProtocol) > 0 {
		resolvedProviderProtocol = providerProtocol[0]
	}
	return RoutableTarget{
		BackendRef:       backendRef,
		ProviderSpec:     providerSpec,
		BaseURL:          baseURL,
		CredentialRef:    credentialRef,
		ProtocolKind:     protocolKind,
		AuthKind:         authKind,
		SelectedFrame:    selectedFrame,
		ProviderProtocol: resolvedProviderProtocol,
	}
}

// Clone protects the execution seam from accidental mutation by caller or callee.
func (t RoutableTarget) Clone() RoutableTarget {
	return NewRoutableTarget(
		t.BackendRef,
		t.ProviderSpec,
		t.BaseURL,
		t.CredentialRef,
		t.ProtocolKind,
		t.AuthKind,
		t.SelectedFrame,
		t.ProviderProtocol,
	)
}

// ProviderID returns the configured provider family key.
func (t RoutableTarget) ProviderID() string {
	return t.ProviderSpec
}
