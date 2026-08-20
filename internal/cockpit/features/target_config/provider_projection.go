package target_config

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

// connectionFromDraft is the single authoring boundary from an incomplete
// Cockpit draft to the durable provider-specific connection sum type.
func connectionFromDraft(draft readmodel.TargetDraft) (routing.Connection, error) {
	credential := strings.TrimSpace(draft.CredentialRef)
	locator := strings.TrimSpace(draft.Locator)
	shape, ok := profile.ConnectionShapeForSpec(draft.ProviderSpec)
	if !ok {
		return nil, fmt.Errorf("unsupported provider %q", draft.ProviderSpec)
	}
	switch shape {
	case routing.ConnectionShapeStandard:
		return routing.FinalizeConnection(routing.ConnectionDraft{
			Provider: draft.ProviderSpec,
			Standard: &routing.StandardConnectionDraft{Locator: locator, Credential: credential},
		}, profile.RoutingConstructionFacts())
	case routing.ConnectionShapeZAI:
		provider, err := routing.ParseProvider(draft.ProviderSpec, profile.SupportsSpec)
		if err != nil {
			return nil, err
		}
		access, err := routing.ParseZAIAccess(draft.ZAIAccess)
		if err != nil {
			return nil, err
		}
		return routing.NewZAIConnection(provider, access, credential)
	case routing.ConnectionShapeBedrock:
		provider, err := routing.ParseProvider(draft.ProviderSpec, profile.SupportsSpec)
		if err != nil {
			return nil, err
		}
		region, err := routing.ParseBedrockRegion(locator)
		if err != nil {
			return nil, err
		}
		return routing.NewBedrockConnection(provider, region, strings.TrimSpace(draft.Endpoint), credential)
	case routing.ConnectionShapeCustom:
		provider, err := routing.ParseProvider(draft.ProviderSpec, profile.SupportsSpec)
		if err != nil {
			return nil, err
		}
		var auth routing.CustomAuth
		if credential != "" {
			header, err := routing.NewCustomHeaderAuth(
				resolvedCredentialHeader(draft.ProviderSpec, draft.CredentialHeader),
				credential,
			)
			if err != nil {
				return nil, err
			}
			auth = header
		}
		return routing.NewCustomConnection(provider, locator, auth)
	default:
		return nil, fmt.Errorf("unsupported connection configuration for provider %q", draft.ProviderSpec)
	}
}

func validateTargetDraftEndpoint(draft readmodel.TargetDraft) error {
	if profile.ProviderID(strings.TrimSpace(draft.ProviderSpec)) != profile.ProviderSpecBedrock {
		return nil
	}
	kind, ok := profile.ProviderProtocolKind(draft.ProviderSpec, draft.ProviderProtocol)
	if !ok {
		return fmt.Errorf("selected provider protocol is unsupported")
	}
	_, err := profile.ResolveBedrockEndpoint(draft.Endpoint, draft.Locator, kind)
	return err
}

// TargetDraftFromReadModel projects the persisted read shape into the typed
// draft used by both create and edit authoring.
func TargetDraftFromReadModel(routeID readmodel.RouteID, target readmodel.TargetReadModel) readmodel.TargetDraft {
	spec := strings.TrimSpace(target.Provider)             // swobu:io-string source=boundary
	locator := strings.TrimSpace(target.BaseURL)           // swobu:io-string source=boundary
	protocol := strings.TrimSpace(target.ProviderProtocol) // swobu:io-string source=boundary
	if normalized, err := profile.DecodeProviderProtocolFromPersistence(spec, protocol); err == nil {
		protocol = normalized
	}
	endpoint := ""
	if profile.ProviderID(spec) == profile.ProviderSpecBedrock {
		// Region is an authored first-class fact surfaced on the readmodel; the
		// endpoint (the complete API base URL) is its own draft field. Neither is
		// parsed from the other.
		locator = strings.TrimSpace(target.BedrockRegion)
		endpoint = strings.TrimSpace(target.BaseURL)
	}
	draft := readmodel.TargetDraft{
		ProviderSpec:     spec,
		ZAIAccess:        strings.TrimSpace(target.ZAIAccess),
		Locator:          locator,
		Endpoint:         endpoint,
		CredentialRef:    strings.TrimSpace(target.CredentialRef), // swobu:io-string source=boundary
		ProviderProtocol: protocol,
		ModelID:          strings.TrimSpace(target.Model),    // swobu:io-string source=boundary
		RouteModelID:     strings.TrimSpace(string(routeID)), // swobu:io-string source=boundary
	}
	if _, derived := profile.DerivedProtocolForSpec(spec); derived {
		draft.ProviderProtocol = ""
	}
	if profile.ProviderID(spec) == profile.ProviderSpecCustom {
		draft.CredentialHeader = strings.TrimSpace(target.AuthHeader) // swobu:io-string source=boundary
	}
	return draft
}

// currentTargetDraft applies the shared transient authoring spine to the
// durable draft. The generic locator buffer also carries an operational base
// URL for fixed profiles so catalog probes have an endpoint, but that runtime
// default is not an operator-authored durable locator. Bedrock owns region and
// explicit endpoint directly on the draft.
func currentTargetDraft(draft readmodel.TargetDraft, locator, modelID, protocol string, routeID readmodel.RouteID) readmodel.TargetDraft {
	if profile.ProviderID(draft.ProviderSpec) == profile.ProviderSpecBedrock {
		// Bedrock's region is already an authored draft fact; BaseURL is its
		// operational inference endpoint and must never replace that region.
	} else if locatorIsAuthorable(draft.ProviderSpec) {
		draft.Locator = strings.TrimSpace(locator) // swobu:io-string source=boundary
	} else {
		draft.Locator = ""
	}
	if _, derived := profile.DerivedProtocolForSpec(draft.ProviderSpec); derived {
		draft.ProviderProtocol = ""
	} else {
		draft.ProviderProtocol = strings.TrimSpace(protocol) // swobu:io-string source=boundary
	}
	draft.ModelID = strings.TrimSpace(modelID)              // swobu:io-string source=boundary
	draft.RouteModelID = strings.TrimSpace(string(routeID)) // swobu:io-string source=boundary
	return draft
}

// locatorIsAuthorable asks the profile-owned durable shape whether the shared
// operational locator buffer belongs in a connection draft. Unknown profiles
// preserve the conservative empty durable value; routing owns rejection of an
// unsupported provider later at its construction boundary.
func locatorIsAuthorable(providerSpec string) bool {
	locator, ok := profile.LocatorSpecForProvider(providerSpec)
	return ok && locator.Kind != profile.LocatorFixed
}

func providerPickerLabel(providerSpec, displayName string) string {
	if label := strings.TrimSpace(displayName); label != "" {
		return label
	}
	return strings.TrimSpace(providerSpec)
}

func providerPickerKeywords(p readmodel.ProviderOptionReadModel) []string {
	keywords := make([]string, 0, 4)
	if spec := strings.TrimSpace(p.ProviderSpec); spec != "" {
		keywords = append(keywords, spec)
	}
	if hint := strings.TrimSpace(p.SetupHint); hint != "" {
		keywords = append(keywords, hint)
	}
	if summary := strings.TrimSpace(profile.ProviderSetupKeywordSummaryForSpec(p.ProviderSpec)); summary != "" {
		keywords = append(keywords, summary)
	}
	return keywords
}

func defaultPlacementForRoute(route readmodel.RouteReadModel) readmodel.PlacementOptionReadModel {
	var anchor readmodel.TargetID
	if len(route.Tiers) > 0 && len(route.Tiers[len(route.Tiers)-1].Targets) > 0 {
		anchor = route.Tiers[len(route.Tiers)-1].Targets[0].ID
	}
	return readmodel.PlacementOptionReadModel{
		Label:        fmt.Sprintf("fallback after step %d", len(route.Tiers)),
		PeerTargetID: anchor,
		Kind:         readmodel.PlacementFallback,
	}
}

func placementOptions(route readmodel.RouteReadModel, mode targetConfigMode, editedTargetID readmodel.TargetID) []readmodel.PlacementOptionReadModel {
	if mode == targetConfigModeEdit {
		route = routeWithoutTarget(route, editedTargetID)
	}
	opts := []readmodel.PlacementOptionReadModel{{Label: "primary", Kind: readmodel.PlacementFallback}}
	for tierIndex, tier := range route.Tiers {
		for _, target := range tier.Targets {
			opts = append(opts, readmodel.PlacementOptionReadModel{Label: "balance with " + fmt.Sprintf("step %d", tierIndex+1), PeerTargetID: target.ID, Kind: readmodel.PlacementBalance})
			break
		}
		if len(tier.Targets) > 0 {
			opts = append(opts, readmodel.PlacementOptionReadModel{Label: fmt.Sprintf("fallback %d", tierIndex+1), PeerTargetID: tier.Targets[0].ID, Kind: readmodel.PlacementFallback})
		}
	}
	return opts
}

// routeWithoutTarget is the topology placement choices actually transform.
// Removing the edited target first also collapses a vacated singleton tier, so
// labels and predecessor anchors describe the route that will exist on save.
func routeWithoutTarget(route readmodel.RouteReadModel, id readmodel.TargetID) readmodel.RouteReadModel {
	out := route
	out.Tiers = make([]readmodel.TierReadModel, 0, len(route.Tiers))
	for _, tier := range route.Tiers {
		targets := make([]readmodel.TargetReadModel, 0, len(tier.Targets))
		for _, target := range tier.Targets {
			if target.ID != id {
				targets = append(targets, target)
			}
		}
		if len(targets) > 0 {
			out.Tiers = append(out.Tiers, readmodel.TierReadModel{Targets: targets})
		}
	}
	return out
}

func currentPlacementForTarget(route readmodel.RouteReadModel, id readmodel.TargetID) readmodel.PlacementOptionReadModel {
	for tierIndex, tier := range route.Tiers {
		for _, target := range tier.Targets {
			if target.ID != id {
				continue
			}
			if len(tier.Targets) > 1 {
				for _, peer := range tier.Targets {
					if peer.ID != id {
						return readmodel.PlacementOptionReadModel{Label: fmt.Sprintf("balance with step %d", tierIndex+1), PeerTargetID: peer.ID, Kind: readmodel.PlacementBalance}
					}
				}
			}
			if tierIndex == 0 {
				return readmodel.PlacementOptionReadModel{Label: "primary", Kind: readmodel.PlacementFallback}
			}
			previous := route.Tiers[tierIndex-1]
			return readmodel.PlacementOptionReadModel{Label: fmt.Sprintf("fallback %d", tierIndex), PeerTargetID: previous.Targets[0].ID, Kind: readmodel.PlacementFallback}
		}
	}
	return defaultPlacementForRoute(route)
}

func placementOptionID(opt readmodel.PlacementOptionReadModel) string {
	return fmt.Sprintf("placement-%s-%d", opt.PeerTargetID, opt.Kind)
}
