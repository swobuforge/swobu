package adapters

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
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
	case target.Connection.ChatGPT != nil:
		out.CredentialRef = target.Connection.ChatGPT.Credential
	case target.Connection.Ollama != nil:
		out.BaseURL = target.Connection.Ollama.BaseURL
	case target.Connection.Azure != nil:
		out.BaseURL = target.Connection.Azure.ProjectEndpoint
		out.CredentialRef = target.Connection.Azure.Credential
	case target.Connection.Bedrock != nil:
		out.BaseURL = profile.BedrockMantleEndpointForRegion(target.Connection.Bedrock.Region)
		if target.Connection.Bedrock.Auth.Profile != nil {
			out.AuthMode = string(profile.AuthModeAWSProfile)
			out.CredentialRef = "profile:" + *target.Connection.Bedrock.Auth.Profile + "@" + target.Connection.Bedrock.Region
		} else if target.Connection.Bedrock.Auth.Environment != nil {
			out.AuthMode = string(profile.AuthModeAWSEnvSession)
		} else if target.Connection.Bedrock.Auth.BearerToken != nil {
			out.AuthMode = string(profile.AuthModeEnv)
			out.CredentialRef = *target.Connection.Bedrock.Auth.BearerToken
		}
	case target.Connection.Custom != nil:
		out.Provider = "openai_compatible"
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
	draft := request.Draft
	target := workspaceapi.TargetDraft{ID: id, Model: strings.TrimSpace(draft.ModelID), Protocol: strings.TrimSpace(draft.ProviderProtocol)}
	credential := strings.TrimSpace(draft.CredentialRef)
	baseURL := strings.TrimSpace(draft.Endpoint.Value)
	switch profile.ProviderID(strings.TrimSpace(draft.ProviderSpec)) {
	case profile.ProviderSpecOpenAI:
		target.Connection.OpenAI = &workspaceapi.CredentialConnection{Credential: credential}
	case profile.ProviderSpecAnthropic:
		target.Connection.Anthropic = &workspaceapi.CredentialConnection{Credential: credential}
	case profile.ProviderSpecOpenRouter:
		target.Connection.OpenRouter = &workspaceapi.CredentialConnection{Credential: credential}
	case profile.ProviderSpecChatGPT:
		target.Connection.ChatGPT = &workspaceapi.CredentialConnection{Credential: credential}
	case profile.ProviderSpecOllama:
		target.Connection.Ollama = &workspaceapi.OllamaConnection{BaseURL: baseURL}
	case profile.ProviderSpecAzure:
		target.Connection.Azure = &workspaceapi.AzureConnection{ProjectEndpoint: baseURL, Credential: credential}
	case profile.ProviderSpecBedrock:
		auth := workspaceapi.BedrockAuth{}
		switch draft.ProviderOptions.Bedrock.AuthMode {
		case string(profile.AuthModeAWSProfile):
			value := draft.ProviderOptions.Bedrock.ProfileName
			auth.Profile = &value
		case string(profile.AuthModeAWSEnvSession):
			auth.Environment = &struct{}{}
		default:
			value := credential
			auth.BearerToken = &value
		}
		target.Connection.Bedrock = &workspaceapi.BedrockConnection{Region: draft.ProviderOptions.Bedrock.Region, Auth: auth}
	case profile.ProviderSpecOpenAICompatible:
		custom := &workspaceapi.CustomConnection{BaseURL: baseURL}
		if credential != "" {
			header := strings.TrimSpace(draft.ProviderOptions.OpenAICompatible.CredentialHeader)
			if header == "" {
				header = "Authorization"
			}
			custom.Header = &workspaceapi.CustomHeader{Name: header, Credential: credential}
		}
		target.Connection.Custom = custom
	default:
		return workspaceapi.TargetDraft{}, errors.New("unsupported provider for target save")
	}
	return target, nil
}
func newTargetID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
func placementFromReadModel(p readmodel.PlacementOptionReadModel) workspaceapi.Placement {
	if p.Kind == readmodel.PlacementBalance && p.PeerTargetID != "" {
		id := string(p.PeerTargetID)
		return workspaceapi.Placement{BalanceWith: &id}
	}
	return workspaceapi.Placement{}
}
