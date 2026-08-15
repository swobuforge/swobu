package readmodel

import (
	"fmt"
	"strings"
)

// ProviderOptionReadModel is one provider shown in the provider picker.
type ProviderOptionReadModel struct {
	ProviderSpec string
	DisplayName  string
	// SetupHint is a short search hint for the provider picker.
	SetupHint string
}

// ModelCatalogReadModel is the advisory model-authoring result of probing a
// provider after setup/auth is ready.
type ModelCatalogReadModel struct {
	Options                  []ModelAuthoringOptionReadModel
	ResolvedProviderProtocol string
	BedrockAuthentication    BedrockAuthenticationEvidence
}

// BedrockAuthenticationKind is the closed Cockpit read-model vocabulary for
// authentication evidence decoded at the operator adapter boundary.
type BedrockAuthenticationKind string

const (
	BedrockAuthenticationExplicitAPIKey BedrockAuthenticationKind = "explicit_api_key"
	BedrockAuthenticationAWSIdentity    BedrockAuthenticationKind = "aws_identity"
)

// BedrockAuthenticationEvidence is typed provider evidence consumed by the
// Cockpit feature; GSX never parses transport JSON.
type BedrockAuthenticationEvidence struct {
	Authentication BedrockAuthenticationKind `json:"authentication"`
	FailureStage   string                    `json:"failure_stage,omitempty"`
	Error          string                    `json:"error,omitempty"`
	AWSIdentity    *AWSIdentityReadModel     `json:"aws_identity,omitempty"`
}

type AWSIdentityReadModel struct {
	State   string
	Account string
	ARN     string
	Error   string
}

// ModelAuthoringOptionReadModel is one selectable model-authoring option from
// a catalog probe.
type ModelAuthoringOptionReadModel struct {
	ID                         string
	Name                       string
	ModelName                  string
	ModelPublisher             string
	ModelVersion               string
	Family                     string
	SupportedProviderProtocols []string
	DefaultProviderProtocol    string
}

// AuthSessionReadModel is the current state of a pending or completed auth
// session for interactive providers such as ChatGPT.
type AuthSessionReadModel struct {
	ProviderSpec  string
	SessionID     string
	State         string
	CredentialRef string
	AuthorizeURL  string
	UserCode      string
	ErrorMessage  string
}

// PlacementKind classifies how a target is positioned within a route.
type PlacementKind int

const (
	PlacementFallback PlacementKind = iota
	PlacementBalance
)

// PlacementOptionReadModel is one available placement choice before target
// creation.
type PlacementOptionReadModel struct {
	Label        string
	PeerTargetID TargetID
	Kind         PlacementKind
}

// PlacementSummary returns a compact display string for the placement option.
func (p PlacementOptionReadModel) Summary() string {
	if label := strings.TrimSpace(p.Label); label != "" {
		return label
	}
	switch p.Kind {
	case PlacementFallback:
		return "fallback after current steps"
	case PlacementBalance:
		return fmt.Sprintf("balance with %s", p.PeerTargetID)
	default:
		return p.Label
	}
}
