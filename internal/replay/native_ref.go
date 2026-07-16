package replay

// NativeRefKind classifies the form of a provider-native replay reference.
type NativeRefKind string

const (
	// NativeRefProviderResponseID is a provider-native response ID such as
	// OpenAI Responses API previous_response_id.
	NativeRefProviderResponseID NativeRefKind = "provider_response_id"
	// NativeRefProviderInteractionID is a provider-native interaction handle
	// used by protocols that track conversations at the provider level.
	NativeRefProviderInteractionID NativeRefKind = "provider_interaction_id"
)

// NativeRef is a validated pointer to provider-native replay state.
//
// Present: the current target can continue provider-native state from this ref.
// Nil:     no compatible provider-native state for this target.
type NativeRef struct {
	ReplayID ID
	Target   TargetKey
	Kind     NativeRefKind
	Value    string
}
