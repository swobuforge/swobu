package ports

import "github.com/swobuforge/swobu/internal/domain/protocolsurface"

type RoutableTarget struct {
	BackendRef string
	// ProviderSpec is the canonical provider family identity.
	ProviderSpec  string
	BaseURL       string
	CredentialRef string
	// ProtocolKind is the concrete selected target protocol surface, not a vendor umbrella.
	ProtocolKind protocolsurface.Kind
	AuthKind     string
	EndpointMode string
}

func NewRoutableTarget(
	backendRef string,
	providerSpec string,
	baseURL string,
	credentialRef string,
	protocolKind protocolsurface.Kind,
	authKind string,
	endpointMode string,
) RoutableTarget {
	return RoutableTarget{
		BackendRef:    backendRef,
		ProviderSpec:  providerSpec,
		BaseURL:       baseURL,
		CredentialRef: credentialRef,
		ProtocolKind:  protocolKind,
		AuthKind:      authKind,
		EndpointMode:  endpointMode,
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
		t.EndpointMode,
	)
}

func (t RoutableTarget) ProviderSpecName() string {
	return t.ProviderSpec
}
