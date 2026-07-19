package provider

import "github.com/swobuforge/swobu/internal/domain/protocolkind"

// ProviderID identifies one fixed provider implementation.
type ProviderID string

// TargetSnapshot is the immutable execution projection of one configured
// routing target. It contains no workspace, route, fallback, or attempt state.
type TargetSnapshot struct {
	TargetID         string
	TargetVersion    uint64
	ProviderSpec     string
	BaseURL          string
	CredentialRef    string
	AuthHeader       string
	Model            string
	ProtocolKind     protocolkind.ProtocolKind
	SelectedFrame    string
	ProviderProtocol string
}

// Clone returns a detached target value.
func (t TargetSnapshot) Clone() TargetSnapshot { return t }

// Equal reports exact target equality. Backend resolution must preserve this
// value so exchange never has two target authorities that can disagree.
func (t TargetSnapshot) Equal(other TargetSnapshot) bool { return t == other }

// ProviderID returns the fixed provider implementation identifier.
func (t TargetSnapshot) ProviderID() string { return t.ProviderSpec }

// NativeContinuation binds one provider-issued handle to this exact routing
// target version. Calling this method is the backend's explicit wire-contract
// opt-in; target snapshots never opt in by themselves.
func (t TargetSnapshot) NativeContinuation(id string) *NativeContinuation {
	if id == "" || t.TargetID == "" || t.TargetVersion == 0 {
		return nil
	}
	return &NativeContinuation{TargetID: t.TargetID, TargetVersion: t.TargetVersion, ID: ContinuationID(id)}
}

// NewTargetSnapshot constructs one provider execution target projection.
func NewTargetSnapshot(targetID, providerSpec, baseURL, credentialRef string, protocol protocolkind.ProtocolKind, selectedFrame string, providerProtocol ...string) TargetSnapshot {
	resolvedProviderProtocol := ""
	if len(providerProtocol) > 0 {
		resolvedProviderProtocol = providerProtocol[0]
	}
	return TargetSnapshot{
		TargetID:         targetID,
		TargetVersion:    1,
		ProviderSpec:     providerSpec,
		BaseURL:          baseURL,
		CredentialRef:    credentialRef,
		ProtocolKind:     protocol,
		SelectedFrame:    selectedFrame,
		ProviderProtocol: resolvedProviderProtocol,
	}
}
