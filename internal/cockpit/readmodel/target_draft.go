package readmodel

// TargetDraft is Cockpit-local incomplete setup state. It is never durable;
// the adapter translates a complete draft into one semantic target command.
type TargetDraft struct {
	ProviderSpec     string
	Locator          string
	CredentialRef    string
	CredentialHeader string
	ProviderProtocol string
	ModelID          string
	RouteModelID     string
}
