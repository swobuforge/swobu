package configstore

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

type documentDTO struct {
	SchemaVersion int                     `yaml:"schema_version"`
	Workspaces    map[string]workspaceDTO `yaml:"workspaces"`
}
type workspaceDTO struct {
	DefaultRoute string              `yaml:"default_route"`
	Routes       map[string]routeDTO `yaml:"routes"`
}
type routeDTO struct {
	Tiers []tierDTO `yaml:"tiers"`
}
type tierDTO struct {
	Targets []targetDTO `yaml:"targets"`
}
type targetDTO struct {
	ID         string        `yaml:"id"`
	Model      string        `yaml:"model"`
	Protocol   string        `yaml:"protocol"`
	Connection connectionDTO `yaml:"connection"`
}

type connectionDTO struct {
	OpenAI     *credentialConnectionDTO `yaml:"openai,omitempty"`
	Anthropic  *credentialConnectionDTO `yaml:"anthropic,omitempty"`
	OpenRouter *credentialConnectionDTO `yaml:"openrouter,omitempty"`
	ChatGPT    *credentialConnectionDTO `yaml:"chatgpt,omitempty"`
	Ollama     *ollamaConnectionDTO     `yaml:"ollama,omitempty"`
	Azure      *azureConnectionDTO      `yaml:"azure,omitempty"`
	Bedrock    *bedrockConnectionDTO    `yaml:"bedrock,omitempty"`
	Custom     *customConnectionDTO     `yaml:"custom,omitempty"`
}
type credentialConnectionDTO struct {
	Credential string `yaml:"credential"`
}
type ollamaConnectionDTO struct {
	BaseURL string `yaml:"base_url,omitempty"`
}
type azureConnectionDTO struct {
	ProjectEndpoint string `yaml:"project_endpoint"`
	Credential      string `yaml:"credential"`
}
type bedrockConnectionDTO struct {
	Region     string `yaml:"region"`
	Credential string `yaml:"credential,omitempty"`
}
type customConnectionDTO struct {
	BaseURL string         `yaml:"base_url"`
	Auth    *customAuthDTO `yaml:"auth,omitempty"`
}
type customAuthDTO struct {
	Header *customHeaderDTO `yaml:"header,omitempty"`
}
type customHeaderDTO struct {
	Name       string `yaml:"name"`
	Credential string `yaml:"credential"`
}

func decode(raw []byte) (routing.Config, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var dto documentDTO
	if err := decoder.Decode(&dto); err != nil {
		return routing.Config{}, fmt.Errorf("decode routing config: %w", readableDecodeError(err))
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return routing.Config{}, fmt.Errorf("decode routing config: multiple YAML documents are unsupported")
		}
		return routing.Config{}, fmt.Errorf("decode routing config: %w", err)
	}
	if dto.SchemaVersion != routing.SchemaVersion {
		return routing.Config{}, fmt.Errorf("unsupported schema_version %d (supported: %d)", dto.SchemaVersion, routing.SchemaVersion)
	}
	workspaces := make([]routing.Workspace, 0, len(dto.Workspaces))
	for rawSlug, encodedWorkspace := range dto.Workspaces {
		slug, err := routing.ParseWorkspaceSlug(rawSlug)
		if err != nil {
			return routing.Config{}, prefixPath(err, "workspaces."+rawSlug)
		}
		defaultRoute, err := routing.ParseRouteName(encodedWorkspace.DefaultRoute)
		if err != nil {
			return routing.Config{}, prefixPath(err, "workspaces."+rawSlug+".default_route")
		}
		routes := make([]routing.Route, 0, len(encodedWorkspace.Routes))
		for rawName, encodedRoute := range encodedWorkspace.Routes {
			name, err := routing.ParseRouteName(rawName)
			if err != nil {
				return routing.Config{}, prefixPath(err, "workspaces."+rawSlug+".routes."+rawName)
			}
			tiers := make([]routing.Tier, 0, len(encodedRoute.Tiers))
			for tierIndex, encodedTier := range encodedRoute.Tiers {
				targets := make([]routing.Target, 0, len(encodedTier.Targets))
				for targetIndex, encodedTarget := range encodedTier.Targets {
					path := fmt.Sprintf("workspaces.%s.routes.%s.tiers[%d].targets[%d]", rawSlug, rawName, tierIndex, targetIndex)
					target, err := decodeTarget(encodedTarget)
					if err != nil {
						return routing.Config{}, prefixPath(err, path)
					}
					targets = append(targets, target)
				}
				tier, err := routing.NewTier(targets)
				if err != nil {
					return routing.Config{}, prefixPath(err, fmt.Sprintf("workspaces.%s.routes.%s.tiers[%d]", rawSlug, rawName, tierIndex))
				}
				tiers = append(tiers, tier)
			}
			route, err := routing.NewRoute(name, tiers)
			if err != nil {
				return routing.Config{}, prefixPath(err, "workspaces."+rawSlug+".routes."+rawName)
			}
			routes = append(routes, route)
		}
		workspace, err := routing.NewWorkspace(slug, defaultRoute, routes)
		if err != nil {
			return routing.Config{}, prefixPath(err, "workspaces."+rawSlug)
		}
		workspaces = append(workspaces, workspace)
	}
	return routing.NewConfig(workspaces)
}

func decodeTarget(dto targetDTO) (routing.Target, error) {
	connection, err := connectionDraft(dto.Connection)
	if err != nil {
		return routing.Target{}, err
	}
	return routing.FinalizeTarget(routing.TargetDraft{
		ID:         dto.ID,
		Model:      dto.Model,
		Protocol:   dto.Protocol,
		Connection: connection,
	}, profile.RoutingConstructionFacts())
}

func connectionDraft(dto connectionDTO) (routing.ConnectionDraft, error) {
	draft := routing.ConnectionDraft{}
	if dto.OpenAI != nil {
		draft.OpenAI = &routing.CredentialConnectionDraft{Credential: dto.OpenAI.Credential}
	}
	if dto.Anthropic != nil {
		draft.Anthropic = &routing.CredentialConnectionDraft{Credential: dto.Anthropic.Credential}
	}
	if dto.OpenRouter != nil {
		draft.OpenRouter = &routing.CredentialConnectionDraft{Credential: dto.OpenRouter.Credential}
	}
	if dto.ChatGPT != nil {
		draft.ChatGPT = &routing.CredentialConnectionDraft{Credential: dto.ChatGPT.Credential}
	}
	if dto.Ollama != nil {
		draft.Ollama = &routing.OllamaConnectionDraft{BaseURL: dto.Ollama.BaseURL}
	}
	if dto.Azure != nil {
		draft.Azure = &routing.AzureConnectionDraft{ProjectEndpoint: dto.Azure.ProjectEndpoint, Credential: dto.Azure.Credential}
	}
	if dto.Bedrock != nil {
		draft.Bedrock = &routing.BedrockConnectionDraft{Region: dto.Bedrock.Region, Credential: dto.Bedrock.Credential}
	}
	if dto.Custom != nil {
		if dto.Custom.Auth != nil && dto.Custom.Auth.Header == nil {
			return routing.ConnectionDraft{}, fmt.Errorf("connection.custom.auth: exactly one auth variant is required")
		}
		draft.Custom = &routing.CustomConnectionDraft{BaseURL: dto.Custom.BaseURL}
		if dto.Custom.Auth != nil && dto.Custom.Auth.Header != nil {
			draft.Custom.Header = &routing.CustomHeaderDraft{Name: dto.Custom.Auth.Header.Name, Credential: dto.Custom.Auth.Header.Credential}
		}
	}
	return draft, nil
}

func encode(config routing.Config) ([]byte, error) {
	dto := documentDTO{SchemaVersion: routing.SchemaVersion, Workspaces: map[string]workspaceDTO{}}
	for _, workspace := range config.Workspaces() {
		encodedWorkspace := workspaceDTO{DefaultRoute: workspace.DefaultRoute().String(), Routes: map[string]routeDTO{}}
		for _, route := range workspace.Routes() {
			encodedRoute := routeDTO{Tiers: make([]tierDTO, 0, len(route.Tiers()))}
			for _, tier := range route.Tiers() {
				targets := tier.Targets()
				sort.Slice(targets, func(i, j int) bool { return targets[i].ID().String() < targets[j].ID().String() })
				encodedTier := tierDTO{Targets: make([]targetDTO, 0, len(targets))}
				for _, target := range targets {
					encodedTarget, err := encodeTarget(target)
					if err != nil {
						return nil, err
					}
					encodedTier.Targets = append(encodedTier.Targets, encodedTarget)
				}
				encodedRoute.Tiers = append(encodedRoute.Tiers, encodedTier)
			}
			encodedWorkspace.Routes[route.Name().String()] = encodedRoute
		}
		dto.Workspaces[workspace.Slug().String()] = encodedWorkspace
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(dto); err != nil {
		return nil, fmt.Errorf("encode routing config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close routing encoder: %w", err)
	}
	return buffer.Bytes(), nil
}

func encodeTarget(target routing.Target) (targetDTO, error) {
	dto := targetDTO{ID: target.ID().String(), Model: target.Model().String(), Protocol: target.Protocol().String()}
	switch c := target.Connection().(type) {
	case routing.OpenAIConnection:
		dto.Connection.OpenAI = &credentialConnectionDTO{Credential: c.Credential().String()}
	case routing.AnthropicConnection:
		dto.Connection.Anthropic = &credentialConnectionDTO{Credential: c.Credential().String()}
	case routing.OpenRouterConnection:
		dto.Connection.OpenRouter = &credentialConnectionDTO{Credential: c.Credential().String()}
	case routing.ChatGPTConnection:
		dto.Connection.ChatGPT = &credentialConnectionDTO{Credential: c.Credential().String()}
	case routing.OllamaConnection:
		u, _ := c.BaseURL()
		dto.Connection.Ollama = &ollamaConnectionDTO{BaseURL: u.String()}
	case routing.AzureConnection:
		dto.Connection.Azure = &azureConnectionDTO{ProjectEndpoint: c.ProjectEndpoint().String(), Credential: c.Credential().String()}
	case routing.BedrockConnection:
		dto.Connection.Bedrock = &bedrockConnectionDTO{Region: c.Region().String(), Credential: c.Credential().String()}
	case routing.CustomConnection:
		encoded := &customConnectionDTO{BaseURL: c.BaseURL().String()}
		if c.Auth() != nil {
			header, ok := c.Auth().(routing.CustomHeaderAuth)
			if !ok {
				return targetDTO{}, fmt.Errorf("encode target %s: unsupported custom auth", target.ID().String())
			}
			encoded.Auth = &customAuthDTO{Header: &customHeaderDTO{Name: header.Name(), Credential: header.Credential().String()}}
		}
		dto.Connection.Custom = encoded
	default:
		return targetDTO{}, fmt.Errorf("encode target %s: unsupported connection %T", target.ID().String(), target.Connection())
	}
	return dto, nil
}

// unknownFieldTypeSuffix strips yaml.v3's internal Go type name (e.g.
// "configstore.documentDTO") from "field X not found in type T" messages.
var unknownFieldTypeSuffix = regexp.MustCompile(`not found in type \S+`)

// readableDecodeError rewrites yaml.v3's strict-mode type errors so they name
// the unrecognized config field instead of an internal struct type; other
// decode errors pass through unchanged.
func readableDecodeError(err error) error {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return err
	}
	cleaned := make([]string, 0, len(typeErr.Errors))
	for _, msg := range typeErr.Errors {
		cleaned = append(cleaned, unknownFieldTypeSuffix.ReplaceAllString(msg, "is not a recognized config field"))
	}
	return fmt.Errorf("config has unrecognized field(s) (the file may be from an older swobu schema):\n  %s", strings.Join(cleaned, "\n  "))
}

func prefixPath(err error, prefix string) error {
	var invariant *routing.InvariantError
	if ok := asInvariant(err, &invariant); ok {
		invariantCopy := *invariant
		if !strings.HasPrefix(invariantCopy.Path, prefix) {
			invariantCopy.Path = prefix + "." + invariantCopy.Path
		}
		return &invariantCopy
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

func asInvariant(err error, target **routing.InvariantError) bool {
	for err != nil {
		if value, ok := err.(*routing.InvariantError); ok {
			*target = value
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
