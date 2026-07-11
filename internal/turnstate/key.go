package turnstate

// TurnStateKind classifies one opaque provider-native replay payload role.
type TurnStateKind string

// TurnStateKey scopes one opaque provider-native replay payload.
type TurnStateKey struct {
	RouteID      string
	ProviderID   string
	ModelID      string
	Conversation string
	Subject      string
	Kind         TurnStateKind
}
