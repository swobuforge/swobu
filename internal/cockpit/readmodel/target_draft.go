package readmodel

import (
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
)

// TargetDraft is Cockpit-local incomplete setup state. It is never durable;
// the adapter translates a complete draft into one semantic target command.
type TargetDraft struct {
	ProviderSpec     string
	Endpoint         ProviderEndpointDraft
	CredentialRef    string
	ProviderProtocol string
	ModelID          string
	RouteModelID     string
	ProviderOptions  ProviderOptionsDraft
}
type ProviderEndpointDraft struct {
	Kind  profile.ProviderEndpointKind
	Value string
}
type ProviderOptionsDraft struct {
	OpenAICompatible OpenAICompatibleOptionsDraft
	Bedrock          BedrockOptionsDraft
}
type OpenAICompatibleOptionsDraft struct{ CredentialHeader string }

func (o OpenAICompatibleOptionsDraft) IsEmpty() bool {
	return strings.TrimSpace(o.CredentialHeader) == ""
}

type BedrockOptionsDraft struct {
	AuthMode    string
	Region      string
	ProfileName string
}

func (o BedrockOptionsDraft) IsEmpty() bool {
	return strings.TrimSpace(o.AuthMode) == "" && strings.TrimSpace(o.Region) == "" && strings.TrimSpace(o.ProfileName) == ""
}
func (o ProviderOptionsDraft) IsEmpty() bool {
	return o.OpenAICompatible.IsEmpty() && o.Bedrock.IsEmpty()
}
