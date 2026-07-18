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

// ModelCatalogReadModel is the result of probing the model catalog for a
// provider after setup/auth is ready.
type ModelCatalogReadModel struct {
	Deployments              []ModelDeploymentReadModel
	ResolvedProviderProtocol string
	Error                    string
}

// ModelDeploymentReadModel is one selectable model option from a catalog probe.
type ModelDeploymentReadModel struct {
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
		return "new fallback tier"
	case PlacementBalance:
		return fmt.Sprintf("same tier as %s", p.PeerTargetID)
	default:
		return p.Label
	}
}
