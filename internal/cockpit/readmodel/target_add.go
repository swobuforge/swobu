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
	SetupHint      string
	DefaultBaseURL string
}

// ProviderSetupReadModel is the projected setup state for a selected provider.
// The adapter derives this from the provider profile plus runtime credential/auth
// state so the workflow does not branch on provider-specific facts.
type ProviderSetupReadModel struct {
	ProviderSpec       string
	DisplayName        string
	DefaultBaseURL     string
	DefaultAuthHeader  string
	CredentialLabel    string
	CredentialRef      string
	CredentialRequired bool
	InteractiveAuth    bool
	AuthModes          []AuthModeReadModel
	RequiresBaseURL    bool
	ReadyForCatalog    bool
	BlockReason        string
}

// AuthModeReadModel describes one auth path available for the provider.
type AuthModeReadModel struct {
	Mode        string
	Kind        string
	Requirement string
	Interactive bool
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
	Label  string
	Rank   int
	Weight int
	Kind   PlacementKind
}

// PlacementSummary returns a compact display string for the placement option.
func (p PlacementOptionReadModel) Summary() string {
	if label := strings.TrimSpace(p.Label); label != "" {
		return label
	}
	switch p.Kind {
	case PlacementFallback:
		return "fallback after last step"
	case PlacementBalance:
		return fmt.Sprintf("balance with step %d", p.Rank)
	default:
		return p.Label
	}
}
