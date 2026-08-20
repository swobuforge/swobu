package adapters

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/routing"
)

func routesFromWorkspace(workspace workspaceapi.Workspace) ([]readmodel.RouteReadModel, error) {
	out := make([]readmodel.RouteReadModel, 0, len(workspace.Routes))
	for _, route := range workspace.Routes {
		projected, err := routeFromWorkspaceRoute(workspace.DefaultRoute, route)
		if err != nil {
			return nil, err
		}
		out = append(out, projected)
	}
	return out, nil
}
func routeFromWorkspaceRoute(defaultRoute string, route workspaceapi.Route) (readmodel.RouteReadModel, error) {
	out := readmodel.RouteReadModel{ID: readmodel.RouteID(route.Name), ModelName: route.Name, State: readmodel.RouteNormal, Default: route.Name == defaultRoute, Enabled: true}
	for _, tier := range route.Tiers {
		projected := readmodel.TierReadModel{}
		for _, target := range tier.Targets {
			projectedTarget, err := targetFromWorkspaceTarget(target)
			if err != nil {
				return readmodel.RouteReadModel{}, fmt.Errorf("project target %q: %w", target.ID, err)
			}
			projected.Targets = append(projected.Targets, projectedTarget)
		}
		out.Tiers = append(out.Tiers, projected)
	}
	return out, nil
}
func targetFromWorkspaceTarget(target workspaceapi.Target) (readmodel.TargetReadModel, error) {
	connection, err := target.Connection.RoutingConnection()
	if err != nil {
		return readmodel.TargetReadModel{}, fmt.Errorf("decode operator connection: %w", err)
	}
	out := readmodel.TargetReadModel{ID: readmodel.TargetID(target.ID), Name: target.ID, Provider: string(connection.Provider()), Model: target.Model, ProviderProtocol: target.Protocol}
	switch connection := connection.(type) {
	case routing.StandardConnection:
		locator, _ := connection.Locator()
		out.BaseURL = locator.String()
		out.CredentialRef = connection.Credential().String()
	case routing.ZAIConnection:
		out.ZAIAccess = string(connection.Access())
		out.CredentialRef = connection.Credential().String()
	case routing.BedrockConnection:
		// The read model preserves the independently authored signing region and
		// complete inference API URL verbatim.
		out.BaseURL = connection.Endpoint()
		out.BedrockRegion = connection.Region().String()
		out.CredentialRef = connection.Credential().String()
	case routing.CustomConnection:
		out.BaseURL = connection.BaseURL().String()
		if auth := connection.Auth(); auth != nil {
			header, ok := auth.(routing.CustomHeaderAuth)
			if !ok {
				return readmodel.TargetReadModel{}, fmt.Errorf("unsupported custom authentication %T", auth)
			}
			out.AuthHeader = header.Name()
			out.CredentialRef = header.Credential().String()
		}
	default:
		return readmodel.TargetReadModel{}, fmt.Errorf("unsupported routing connection %T", connection)
	}
	return out, nil
}
func targetFromWorkspace(workspace workspaceapi.Workspace, id string) (readmodel.TargetReadModel, error) {
	for _, route := range workspace.Routes {
		for _, tier := range route.Tiers {
			for _, target := range tier.Targets {
				if target.ID == id {
					return targetFromWorkspaceTarget(target)
				}
			}
		}
	}
	return readmodel.TargetReadModel{}, errors.New("committed target missing from workspace response")
}

func targetFromSaveRequest(request ports.SaveTargetRequest, id string) (workspaceapi.TargetDraft, error) {
	if request.Connection == nil {
		return workspaceapi.TargetDraft{}, errors.New("validated target connection is required")
	}
	return workspaceapi.TargetDraft{
		ID: id, Model: strings.TrimSpace(request.ModelID), Protocol: strings.TrimSpace(request.Protocol),
		Connection: workspaceapi.ConnectionFromRouting(request.Connection),
	}, nil
}
func newTargetID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("tgt_%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}
func routeSpecWithTarget(spec workspaceapi.RouteSpec, target workspaceapi.TargetDraft, placement readmodel.PlacementOptionReadModel) (workspaceapi.RouteSpec, error) {
	spec, _ = removeTargetFromSpec(spec, target.ID)
	if placement.Kind == readmodel.PlacementBalance {
		for i := range spec.Tiers {
			for _, peer := range spec.Tiers[i].Targets {
				if peer.ID == string(placement.PeerTargetID) {
					spec.Tiers[i].Targets = append(spec.Tiers[i].Targets, target)
					return spec, nil
				}
			}
		}
		return workspaceapi.RouteSpec{}, fmt.Errorf("routing peer %q is missing", placement.PeerTargetID)
	}
	insertAt := 0
	if placement.PeerTargetID != "" {
		insertAt = -1
		for i, tier := range spec.Tiers {
			for _, peer := range tier.Targets {
				if peer.ID == string(placement.PeerTargetID) {
					insertAt = i + 1
					break
				}
			}
		}
		if insertAt < 0 {
			return workspaceapi.RouteSpec{}, fmt.Errorf("routing predecessor %q is missing", placement.PeerTargetID)
		}
	}
	tier := workspaceapi.TierSpec{Targets: []workspaceapi.TargetDraft{target}}
	spec.Tiers = append(spec.Tiers, workspaceapi.TierSpec{})
	copy(spec.Tiers[insertAt+1:], spec.Tiers[insertAt:])
	spec.Tiers[insertAt] = tier
	return spec, nil
}

func routeSpecForTopology(current workspaceapi.RouteSpec, desired readmodel.RouteReadModel) (workspaceapi.RouteSpec, error) {
	byID := map[string]workspaceapi.TargetDraft{}
	for _, tier := range current.Tiers {
		for _, target := range tier.Targets {
			byID[target.ID] = target
		}
	}
	spec := workspaceapi.RouteSpec{Tiers: make([]workspaceapi.TierSpec, len(desired.Tiers))}
	for tierIndex, tier := range desired.Tiers {
		for _, target := range tier.Targets {
			draft, ok := byID[string(target.ID)]
			if !ok {
				return workspaceapi.RouteSpec{}, fmt.Errorf("target %q is missing", target.ID)
			}
			spec.Tiers[tierIndex].Targets = append(spec.Tiers[tierIndex].Targets, draft)
		}
	}
	if len(spec.Tiers) == 0 {
		return workspaceapi.RouteSpec{}, errors.New("delete the route instead")
	}
	return spec, nil
}

func removeTargetFromSpec(spec workspaceapi.RouteSpec, targetID string) (workspaceapi.RouteSpec, bool) {
	found := false
	tiers := make([]workspaceapi.TierSpec, 0, len(spec.Tiers))
	for _, tier := range spec.Tiers {
		targets := make([]workspaceapi.TargetDraft, 0, len(tier.Targets))
		for _, target := range tier.Targets {
			if target.ID == targetID {
				found = true
				continue
			}
			targets = append(targets, target)
		}
		if len(targets) > 0 {
			tiers = append(tiers, workspaceapi.TierSpec{Targets: targets})
		}
	}
	spec.Tiers = tiers
	return spec, found
}
