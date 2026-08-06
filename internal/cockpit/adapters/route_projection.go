package adapters

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func routesFromWorkspace(workspace workspaceapi.Workspace) []readmodel.RouteReadModel {
	out := make([]readmodel.RouteReadModel, 0, len(workspace.Routes))
	for _, route := range workspace.Routes {
		out = append(out, routeFromWorkspaceRoute(workspace.DefaultRoute, route))
	}
	return out
}
func routeFromWorkspaceRoute(defaultRoute string, route workspaceapi.Route) readmodel.RouteReadModel {
	out := readmodel.RouteReadModel{ID: readmodel.RouteID(route.Name), ModelName: route.Name, State: readmodel.RouteNormal, Default: route.Name == defaultRoute, Enabled: true}
	for _, tier := range route.Tiers {
		projected := readmodel.TierReadModel{}
		for _, target := range tier.Targets {
			projected.Targets = append(projected.Targets, targetFromWorkspaceTarget(target))
		}
		out.Tiers = append(out.Tiers, projected)
	}
	return out
}
func targetFromWorkspaceTarget(target workspaceapi.Target) readmodel.TargetReadModel {
	out := readmodel.TargetReadModel{ID: readmodel.TargetID(target.ID), Name: target.ID, Provider: target.Provider, Model: target.Model, ProviderProtocol: target.Protocol}
	switch {
	case target.Connection.OpenAI != nil:
		out.CredentialRef = target.Connection.OpenAI.Credential
	case target.Connection.Anthropic != nil:
		out.CredentialRef = target.Connection.Anthropic.Credential
	case target.Connection.OpenRouter != nil:
		out.CredentialRef = target.Connection.OpenRouter.Credential
	case target.Connection.ZAI != nil:
		out.ZAIAccess = target.Connection.ZAI.Access
		out.CredentialRef = target.Connection.ZAI.Credential
	case target.Connection.ChatGPT != nil:
		out.CredentialRef = target.Connection.ChatGPT.Credential
	case target.Connection.Ollama != nil:
		out.BaseURL = target.Connection.Ollama.BaseURL
	case target.Connection.Azure != nil:
		out.BaseURL = target.Connection.Azure.ProjectEndpoint
		out.CredentialRef = target.Connection.Azure.Credential
	case target.Connection.Bedrock != nil:
		// The read model preserves authored truth: region as a first-class field
		// and the explicit endpoint verbatim, empty when the endpoint is derived.
		// The Cockpit derives the effective URL it will display from (region,
		// endpoint) via profile.EffectiveBedrockAPIURL; the read model never
		// materializes that derivation, so the durable fact has one shape.
		out.BaseURL = target.Connection.Bedrock.Endpoint
		out.BedrockRegion = target.Connection.Bedrock.Region
		out.CredentialRef = target.Connection.Bedrock.Credential
	case target.Connection.Custom != nil:
		out.BaseURL = target.Connection.Custom.BaseURL
		if target.Connection.Custom.Header != nil {
			out.AuthHeader = target.Connection.Custom.Header.Name
			out.CredentialRef = target.Connection.Custom.Header.Credential
		}
	}
	return out
}
func targetFromWorkspace(workspace workspaceapi.Workspace, id string) (readmodel.TargetReadModel, error) {
	for _, route := range workspace.Routes {
		for _, tier := range route.Tiers {
			for _, target := range tier.Targets {
				if target.ID == id {
					return targetFromWorkspaceTarget(target), nil
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
func placementFromReadModel(p readmodel.PlacementOptionReadModel) workspaceapi.Placement {
	if p.Kind == readmodel.PlacementBalance && p.PeerTargetID != "" {
		id := string(p.PeerTargetID)
		return workspaceapi.Placement{BalanceWith: &id}
	}
	return workspaceapi.Placement{}
}
