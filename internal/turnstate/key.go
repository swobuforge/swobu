package turnstate

// Kind classifies one opaque continuation payload role.
type Kind string

// Key scopes one opaque continuation payload.
type Key struct {
	RouteID      string
	ProviderID   string
	ModelID      string
	Conversation string
	Subject      string
	Kind         Kind
}
