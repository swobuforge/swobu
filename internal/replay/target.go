package replay

import "github.com/swobuforge/swobu/internal/domain/protocolkind"

// TargetKey identifies the exact backend that produced a native replay ref.
// Native provider state is valid only for the exact backend namespace that
// produced it. AuthScope is intentionally derived from the immutable
// credential ref contract enforced by endpoint intent; if the control plane
// can repoint credentials in place, native replay equality is not trustworthy.
type TargetKey struct {
	ProviderSpec     string
	Protocol         protocolkind.ProtocolKind
	ProviderProtocol string
	BaseURL          string
	// AuthScope is the stable auth namespace. Replay relies on the control
	// plane to treat the credential ref as immutable once bound.
	AuthScope string
	AuthKind  string
	ModelID   string
}

// TargetKeyFromRoutableTarget builds a TargetKey from exchange routing state.
// This lives here so the replay package does not depend on exchange types.
func TargetKeyFromRoutableTarget(
	providerSpec string,
	protocol protocolkind.ProtocolKind,
	providerProtocol string,
	baseURL string,
	authScope string,
	authKind string,
	modelID string,
) TargetKey {
	return TargetKey{
		ProviderSpec:     providerSpec,
		Protocol:         protocol,
		ProviderProtocol: providerProtocol,
		BaseURL:          baseURL,
		AuthScope:        authScope,
		AuthKind:         authKind,
		ModelID:          modelID,
	}
}

// Equal returns true only when every field matches exactly.
// There is no compatibility matrix. Exact equality is the only valid rule.
func (k TargetKey) Equal(other TargetKey) bool {
	return k == other
}
