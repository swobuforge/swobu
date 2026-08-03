// Package workspaces owns operator queries and semantic commands over the
// latest routing aggregate. It converts transport drafts to routing.TargetDraft
// without interpreting providers and never accepts caller-authored snapshots.
package workspaces

import (
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

type Workspace struct {
	Slug         string  `json:"slug"`
	DefaultRoute string  `json:"default_route"`
	Routes       []Route `json:"routes"`
}
type WorkspaceSummary struct {
	Slug         string `json:"slug"`
	DefaultRoute string `json:"default_route"`
	RouteCount   int    `json:"route_count"`
}
type Route struct {
	Name  string `json:"name"`
	Tiers []Tier `json:"tiers"`
}
type Tier struct {
	Targets []Target `json:"targets"`
}
type Target struct {
	ID         string     `json:"id"`
	Model      string     `json:"model"`
	Protocol   string     `json:"protocol"`
	Provider   string     `json:"provider"`
	Connection Connection `json:"connection"`
}

// TargetDraft is create-command input. Provider identity is derived solely
// from the one populated connection arm.
type TargetDraft struct {
	ID         string     `json:"id"`
	Model      string     `json:"model"`
	Protocol   string     `json:"protocol"`
	Connection Connection `json:"connection"`
}

// TargetSettingsDraft is update-command input; the request path is the only
// stable target identity source.
type TargetSettingsDraft struct {
	Model      string     `json:"model"`
	Protocol   string     `json:"protocol"`
	Connection Connection `json:"connection"`
}

type Connection struct {
	OpenAI     *CredentialConnection `json:"openai,omitempty"`
	Anthropic  *CredentialConnection `json:"anthropic,omitempty"`
	DeepSeek   *CredentialConnection `json:"deepseek,omitempty"`
	OpenRouter *CredentialConnection `json:"openrouter,omitempty"`
	ZAI        *ZAIConnection        `json:"zai,omitempty"`
	ChatGPT    *CredentialConnection `json:"chatgpt,omitempty"`
	Ollama     *OllamaConnection     `json:"ollama,omitempty"`
	Azure      *AzureConnection      `json:"azure,omitempty"`
	Bedrock    *BedrockConnection    `json:"bedrock,omitempty"`
	Custom     *CustomConnection     `json:"custom,omitempty"`
}
type CredentialConnection struct {
	Credential string `json:"credential"`
}
type ZAIConnection struct {
	Access     string `json:"access"`
	Credential string `json:"credential"`
}
type OllamaConnection struct {
	BaseURL string `json:"base_url,omitempty"`
}
type AzureConnection struct {
	ProjectEndpoint string `json:"project_endpoint"`
	Credential      string `json:"credential"`
}
type BedrockConnection struct {
	Region     string `json:"region"`
	Credential string `json:"credential,omitempty"`
}
type CustomConnection struct {
	BaseURL string        `json:"base_url"`
	Header  *CustomHeader `json:"header,omitempty"`
}
type CustomHeader struct {
	Name       string `json:"name"`
	Credential string `json:"credential"`
}

func projectWorkspace(workspace routing.Workspace) Workspace {
	out := Workspace{Slug: workspace.Slug().String(), DefaultRoute: workspace.DefaultRoute().String()}
	for _, route := range workspace.Routes() {
		projected := Route{Name: route.Name().String()}
		for _, tier := range route.Tiers() {
			projectedTier := Tier{}
			for _, target := range tier.Targets() {
				projectedTier.Targets = append(projectedTier.Targets, projectTarget(target))
			}
			sort.Slice(projectedTier.Targets, func(i, j int) bool { return projectedTier.Targets[i].ID < projectedTier.Targets[j].ID })
			projected.Tiers = append(projected.Tiers, projectedTier)
		}
		out.Routes = append(out.Routes, projected)
	}
	sort.Slice(out.Routes, func(i, j int) bool { return out.Routes[i].Name < out.Routes[j].Name })
	return out
}

func projectTarget(target routing.Target) Target {
	// Read models expose effective runtime truth. Single-protocol providers omit
	// protocol from authoring and persisted config, but diagnostics and external
	// operator clients still receive the effective routed protocol here.
	out := Target{ID: target.ID().String(), Model: target.Model().String(), Protocol: target.Protocol().String(), Provider: string(target.Provider())}
	out.Connection = ConnectionFromRouting(target.Connection())
	return out
}

// ConnectionFromRouting is the shared routing-to-operator transport codec used
// by both target persistence and target probing.
func ConnectionFromRouting(connection routing.Connection) Connection {
	out := Connection{}
	switch c := connection.(type) {
	case routing.APIKeyConnection:
		credential := &CredentialConnection{Credential: c.Credential().String()}
		switch c.Provider() {
		case routing.ProviderOpenAI:
			out.OpenAI = credential
		case routing.ProviderAnthropic:
			out.Anthropic = credential
		case routing.ProviderDeepSeek:
			out.DeepSeek = credential
		case routing.ProviderOpenRouter:
			out.OpenRouter = credential
		case routing.ProviderChatGPT:
			out.ChatGPT = credential
		}
	case routing.ZAIConnection:
		out.ZAI = &ZAIConnection{Access: string(c.Access()), Credential: c.Credential().String()}
	case routing.OllamaConnection:
		value, _ := c.BaseURL()
		out.Ollama = &OllamaConnection{BaseURL: value.String()}
	case routing.AzureConnection:
		out.Azure = &AzureConnection{ProjectEndpoint: c.ProjectEndpoint().String(), Credential: c.Credential().String()}
	case routing.BedrockConnection:
		out.Bedrock = &BedrockConnection{Region: c.Region().String(), Credential: c.Credential().String()}
	case routing.CustomConnection:
		custom := &CustomConnection{BaseURL: c.BaseURL().String()}
		if c.Auth() != nil {
			header := c.Auth().(routing.CustomHeaderAuth)
			custom.Header = &CustomHeader{Name: header.Name(), Credential: header.Credential().String()}
		}
		out.Custom = custom
	}
	return out
}

func (t TargetDraft) routingTarget() (routing.Target, error) {
	return finalizeTargetDraft(t.ID, t.Model, t.Protocol, t.Connection)
}

func (t TargetSettingsDraft) routingTarget(id string) (routing.Target, error) {
	return finalizeTargetDraft(id, t.Model, t.Protocol, t.Connection)
}

func finalizeTargetDraft(id, model, protocol string, connection Connection) (routing.Target, error) {
	return routing.FinalizeTarget(routing.TargetDraft{
		ID: id, Model: model, Protocol: protocol, Connection: connection.routingDraft(),
	}, profile.RoutingConstructionFacts())
}

func (c Connection) routingDraft() routing.ConnectionDraft {
	draft := routing.ConnectionDraft{}
	if c.OpenAI != nil {
		draft.APIKey = &routing.APIKeyConnectionDraft{Provider: routing.ProviderOpenAI, Credential: c.OpenAI.Credential}
	}
	if c.Anthropic != nil {
		draft.APIKey = &routing.APIKeyConnectionDraft{Provider: routing.ProviderAnthropic, Credential: c.Anthropic.Credential}
	}
	if c.DeepSeek != nil {
		draft.APIKey = &routing.APIKeyConnectionDraft{Provider: routing.ProviderDeepSeek, Credential: c.DeepSeek.Credential}
	}
	if c.OpenRouter != nil {
		draft.APIKey = &routing.APIKeyConnectionDraft{Provider: routing.ProviderOpenRouter, Credential: c.OpenRouter.Credential}
	}
	if c.ZAI != nil {
		draft.ZAI = &routing.ZAIConnectionDraft{Access: c.ZAI.Access, Credential: c.ZAI.Credential}
	}
	if c.ChatGPT != nil {
		draft.APIKey = &routing.APIKeyConnectionDraft{Provider: routing.ProviderChatGPT, Credential: c.ChatGPT.Credential}
	}
	if c.Ollama != nil {
		draft.Ollama = &routing.OllamaConnectionDraft{BaseURL: c.Ollama.BaseURL}
	}
	if c.Azure != nil {
		draft.Azure = &routing.AzureConnectionDraft{ProjectEndpoint: c.Azure.ProjectEndpoint, Credential: c.Azure.Credential}
	}
	if c.Bedrock != nil {
		draft.Bedrock = &routing.BedrockConnectionDraft{Region: c.Bedrock.Region, Credential: c.Bedrock.Credential}
	}
	if c.Custom != nil {
		draft.Custom = &routing.CustomConnectionDraft{BaseURL: c.Custom.BaseURL}
		if c.Custom.Header != nil {
			draft.Custom.Header = &routing.CustomHeaderDraft{Name: c.Custom.Header.Name, Credential: c.Custom.Header.Credential}
		}
	}
	return draft
}

// RoutingConnection parses the operator connection union through the routing
// domain's single connection finalizer.
func (c Connection) RoutingConnection() (routing.Connection, error) {
	return routing.FinalizeConnection(c.routingDraft(), profile.RoutingConstructionFacts())
}

func normalize(raw string) string { return strings.TrimSpace(raw) }
