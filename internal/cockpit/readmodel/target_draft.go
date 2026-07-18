package readmodel

import "strings"

// TargetDraft is Cockpit-local incomplete setup state. It is never durable;
// the adapter translates a complete draft into one semantic target command.
type TargetDraft struct {
	ProviderSpec     string
	Locator          string
	CredentialRef    string
	ProviderProtocol string
	ModelID          string
	RouteModelID     string
	ProviderOptions  ProviderOptionsDraft
}
type ProviderOptionsDraft struct {
	OpenAICompatible OpenAICompatibleOptionsDraft
}
type OpenAICompatibleOptionsDraft struct{ CredentialHeader string }

func (o OpenAICompatibleOptionsDraft) IsEmpty() bool {
	return strings.TrimSpace(o.CredentialHeader) == ""
}

func (o ProviderOptionsDraft) IsEmpty() bool {
	return o.OpenAICompatible.IsEmpty()
}
