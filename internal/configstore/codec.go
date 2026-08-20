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
	DefaultRoute      string              `yaml:"default_route"`
	TargetGenerations map[string]uint64   `yaml:"target_generations,omitempty"`
	Routes            map[string]routeDTO `yaml:"routes"`
}
type routeDTO struct {
	Tiers []tierDTO `yaml:"tiers"`
}
type tierDTO struct {
	Targets []targetDTO `yaml:"targets"`
}
type targetDTO struct {
	ID         string        `yaml:"id"`
	Version    *uint64       `yaml:"version,omitempty"`
	Model      string        `yaml:"model"`
	Protocol   string        `yaml:"protocol,omitempty"`
	Connection connectionDTO `yaml:"connection"`
}

// connectionDTO preserves the provider-keyed YAML contract while keeping its
// Go representation free of catalog-sized provider fields. The profile chooses
// the stable child grammar from the provider's durable connection shape.
type connectionDTO struct {
	Draft routing.ConnectionDraft
}

type credentialConnectionDTO struct {
	Credential string `yaml:"credential,omitempty"`
}
type zaiConnectionDTO struct {
	Access     string `yaml:"access"`
	Credential string `yaml:"credential"`
}
type endpointCredentialConnectionDTO struct {
	BaseURL    string `yaml:"base_url,omitempty"`
	Credential string `yaml:"credential,omitempty"`
}
type azureConnectionDTO struct {
	ProjectEndpoint string `yaml:"project_endpoint"`
	Credential      string `yaml:"credential"`
}
type bedrockConnectionDTO struct {
	Region     string `yaml:"region"`
	Endpoint   string `yaml:"endpoint,omitempty"`
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

func (dto *connectionDTO) UnmarshalYAML(value *yaml.Node) error {
	provider, child, err := singleProviderYAMLNode(value)
	if err != nil {
		return err
	}
	shape, ok := profile.ConnectionShapeForSpec(provider)
	if !ok {
		return fmt.Errorf("connection.%s: provider is unsupported", provider)
	}
	draft := routing.ConnectionDraft{Provider: provider}
	switch shape {
	case routing.ConnectionShapeStandard:
		locator, _ := profile.LocatorSpecForProvider(provider)
		entry, _ := profile.ProfileForSpec(provider)
		if locator.Kind == profile.LocatorAzureProject {
			var encoded azureConnectionDTO
			if err := decodeStrictYAMLNode(child, &encoded, standardConnectionYAMLFields(locator, entry.Credential)...); err != nil {
				return fmt.Errorf("connection.%s: %w", provider, err)
			}
			draft.Standard = &routing.StandardConnectionDraft{Locator: encoded.ProjectEndpoint, Credential: encoded.Credential}
		} else if locator.Kind == profile.LocatorFixed {
			var encoded credentialConnectionDTO
			if err := decodeStrictYAMLNode(child, &encoded, standardConnectionYAMLFields(locator, entry.Credential)...); err != nil {
				return fmt.Errorf("connection.%s: %w", provider, err)
			}
			draft.Standard = &routing.StandardConnectionDraft{Credential: encoded.Credential}
		} else {
			var encoded endpointCredentialConnectionDTO
			if err := decodeStrictYAMLNode(child, &encoded, standardConnectionYAMLFields(locator, entry.Credential)...); err != nil {
				return fmt.Errorf("connection.%s: %w", provider, err)
			}
			draft.Standard = &routing.StandardConnectionDraft{Locator: encoded.BaseURL, Credential: encoded.Credential}
		}
	case routing.ConnectionShapeZAI:
		var encoded zaiConnectionDTO
		if err := decodeStrictYAMLNode(child, &encoded, "access", "credential"); err != nil {
			return fmt.Errorf("connection.%s: %w", provider, err)
		}
		draft.ZAI = &routing.ZAIConnectionDraft{Access: encoded.Access, Credential: encoded.Credential}
	case routing.ConnectionShapeBedrock:
		var encoded bedrockConnectionDTO
		if err := decodeStrictYAMLNode(child, &encoded, "region", "endpoint", "credential"); err != nil {
			return fmt.Errorf("connection.%s: %w", provider, err)
		}
		draft.Bedrock = &routing.BedrockConnectionDraft{Region: encoded.Region, Endpoint: encoded.Endpoint, Credential: encoded.Credential}
	case routing.ConnectionShapeCustom:
		var encoded customConnectionDTO
		if err := decodeStrictYAMLNode(child, &encoded, "base_url", "auth"); err != nil {
			return fmt.Errorf("connection.%s: %w", provider, err)
		}
		if encoded.Auth != nil && encoded.Auth.Header == nil {
			return fmt.Errorf("connection.%s.auth: exactly one auth variant is required", provider)
		}
		draft.Custom = &routing.CustomConnectionDraft{BaseURL: encoded.BaseURL}
		if encoded.Auth != nil {
			if err := validateCustomHeaderYAML(child); err != nil {
				return fmt.Errorf("connection.%s: %w", provider, err)
			}
			draft.Custom.Header = &routing.CustomHeaderDraft{Name: encoded.Auth.Header.Name, Credential: encoded.Auth.Header.Credential}
		}
	default:
		return fmt.Errorf("connection.%s: provider connection requirements are unsupported", provider)
	}
	dto.Draft = draft
	return nil
}

func standardConnectionYAMLFields(locator profile.LocatorSpec, credential profile.CredentialSpec) []string {
	fields := make([]string, 0, 2)
	switch locator.Kind {
	case profile.LocatorAzureProject:
		fields = append(fields, "project_endpoint")
	case profile.LocatorBaseURL:
		fields = append(fields, "base_url")
	}
	if credential.Requirement != profile.CredentialUnsupported {
		fields = append(fields, "credential")
	}
	return fields
}

func (dto connectionDTO) MarshalYAML() (any, error) {
	connection, err := routing.FinalizeConnection(dto.Draft, profile.RoutingConstructionFacts())
	if err != nil {
		return nil, err
	}
	provider := string(connection.Provider())
	switch connection := connection.(type) {
	case routing.StandardConnection:
		locator, _ := profile.LocatorSpecForProvider(provider)
		if locator.Kind == profile.LocatorAzureProject {
			value, _ := connection.Locator()
			return map[string]any{provider: azureConnectionDTO{ProjectEndpoint: value.String(), Credential: connection.Credential().String()}}, nil
		}
		if locator.Kind == profile.LocatorFixed {
			return map[string]any{provider: credentialConnectionDTO{Credential: connection.Credential().String()}}, nil
		}
		value, _ := connection.Locator()
		return map[string]any{provider: endpointCredentialConnectionDTO{BaseURL: value.String(), Credential: connection.Credential().String()}}, nil
	case routing.ZAIConnection:
		return map[string]any{provider: zaiConnectionDTO{Access: string(connection.Access()), Credential: connection.Credential().String()}}, nil
	case routing.BedrockConnection:
		return map[string]any{provider: bedrockConnectionDTO{Region: connection.Region().String(), Endpoint: connection.Endpoint(), Credential: connection.Credential().String()}}, nil
	case routing.CustomConnection:
		encoded := customConnectionDTO{BaseURL: connection.BaseURL().String()}
		if auth := connection.Auth(); auth != nil {
			header, ok := auth.(routing.CustomHeaderAuth)
			if !ok {
				return nil, fmt.Errorf("unsupported custom auth %T", auth)
			}
			encoded.Auth = &customAuthDTO{Header: &customHeaderDTO{Name: header.Name(), Credential: header.Credential().String()}}
		}
		return map[string]any{provider: encoded}, nil
	default:
		return nil, fmt.Errorf("unsupported routing connection %T", connection)
	}
}

func singleProviderYAMLNode(value *yaml.Node) (string, *yaml.Node, error) {
	if value.Kind != yaml.MappingNode || len(value.Content) != 2 || value.Content[0].Kind != yaml.ScalarNode {
		return "", nil, fmt.Errorf("connection must contain exactly one provider key")
	}
	provider := strings.TrimSpace(value.Content[0].Value)
	if !profile.SupportsSpec(provider) {
		return "", nil, fmt.Errorf("connection.%s: provider is unsupported", provider)
	}
	return provider, value.Content[1], nil
}

func decodeStrictYAMLNode(value *yaml.Node, target any, allowed ...string) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("connection payload must be a mapping")
	}
	known := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		known[field] = struct{}{}
	}
	seen := make(map[string]struct{}, len(allowed))
	for index := 0; index < len(value.Content); index += 2 {
		key := value.Content[index]
		if key.Kind != yaml.ScalarNode {
			return fmt.Errorf("connection field name must be a string")
		}
		if _, exists := known[key.Value]; !exists {
			return fmt.Errorf("field %q is not recognized", key.Value)
		}
		if _, duplicate := seen[key.Value]; duplicate {
			return fmt.Errorf("field %q is duplicated", key.Value)
		}
		seen[key.Value] = struct{}{}
	}
	return value.Decode(target)
}

func validateCustomHeaderYAML(value *yaml.Node) error {
	for index := 0; index < len(value.Content); index += 2 {
		if value.Content[index].Value != "auth" {
			continue
		}
		auth := value.Content[index+1]
		if err := validateMappingFields(auth, "header"); err != nil {
			return fmt.Errorf("auth: %w", err)
		}
		if len(auth.Content) == 2 && auth.Content[0].Value == "header" {
			if err := validateMappingFields(auth.Content[1], "name", "credential"); err != nil {
				return fmt.Errorf("auth.header: %w", err)
			}
		}
	}
	return nil
}

func validateMappingFields(value *yaml.Node, allowed ...string) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("connection payload must be a mapping")
	}
	known := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		known[field] = struct{}{}
	}
	seen := make(map[string]struct{}, len(allowed))
	for index := 0; index < len(value.Content); index += 2 {
		key := value.Content[index]
		if key.Kind != yaml.ScalarNode {
			return fmt.Errorf("connection field name must be a string")
		}
		if _, exists := known[key.Value]; !exists {
			return fmt.Errorf("field %q is not recognized", key.Value)
		}
		if _, duplicate := seen[key.Value]; duplicate {
			return fmt.Errorf("field %q is duplicated", key.Value)
		}
		seen[key.Value] = struct{}{}
	}
	return nil
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
		generations := make(map[routing.TargetID]routing.TargetVersion, len(encodedWorkspace.TargetGenerations))
		for rawID, rawVersion := range encodedWorkspace.TargetGenerations {
			id, err := routing.ParseTargetID(rawID)
			if err != nil {
				return routing.Config{}, prefixPath(err, "workspaces."+rawSlug+".target_generations."+rawID)
			}
			if rawVersion == 0 {
				return routing.Config{}, fmt.Errorf("workspaces.%s.target_generations.%s: generation must be greater than zero", rawSlug, rawID)
			}
			generations[id] = routing.TargetVersion(rawVersion)
		}
		workspace, err := routing.RestoreWorkspace(slug, defaultRoute, routes, generations)
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
	target, err := routing.FinalizeTarget(routing.TargetDraft{
		ID:         dto.ID,
		Model:      dto.Model,
		Protocol:   dto.Protocol,
		Connection: connection,
	}, profile.RoutingConstructionFacts())
	if err != nil {
		return routing.Target{}, err
	}
	version := routing.TargetVersion(1)
	if dto.Version == nil {
		// Schema v1 predates durable target versions. Reading an absent value as
		// the initial revision is the one-way migration; every subsequent write
		// emits the explicit daemon-owned value.
	} else if *dto.Version == 0 {
		return routing.Target{}, fmt.Errorf("target.version: must be greater than zero")
	} else {
		version = routing.TargetVersion(*dto.Version)
	}
	return routing.RestoreTarget(target.ID(), version, target.Model(), target.Protocol(), target.Connection())
}

func connectionDraft(dto connectionDTO) (routing.ConnectionDraft, error) {
	return dto.Draft, nil
}

func encode(config routing.Config) ([]byte, error) {
	dto := documentDTO{SchemaVersion: routing.SchemaVersion, Workspaces: map[string]workspaceDTO{}}
	for _, workspace := range config.Workspaces() {
		encodedWorkspace := workspaceDTO{DefaultRoute: workspace.DefaultRoute().String(), TargetGenerations: map[string]uint64{}, Routes: map[string]routeDTO{}}
		for id, version := range workspace.TargetGenerations() {
			encodedWorkspace.TargetGenerations[id.String()] = uint64(version)
		}
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
	version := uint64(target.Version())
	dto := targetDTO{ID: target.ID().String(), Version: &version, Model: target.Model().String(), Protocol: target.Protocol().String()}
	if _, derived := profile.DerivedProtocolForSpec(string(target.Provider())); derived {
		dto.Protocol = ""
	}
	draft, err := connectionDraftFromRouting(target.Connection())
	if err != nil {
		return targetDTO{}, fmt.Errorf("encode target %s: %w", target.ID().String(), err)
	}
	dto.Connection = connectionDTO{Draft: draft}
	return dto, nil
}

func connectionDraftFromRouting(connection routing.Connection) (routing.ConnectionDraft, error) {
	draft := routing.ConnectionDraft{Provider: string(connection.Provider())}
	switch connection := connection.(type) {
	case routing.StandardConnection:
		locator, _ := connection.Locator()
		draft.Standard = &routing.StandardConnectionDraft{Locator: locator.String(), Credential: connection.Credential().String()}
	case routing.ZAIConnection:
		draft.ZAI = &routing.ZAIConnectionDraft{Access: string(connection.Access()), Credential: connection.Credential().String()}
	case routing.BedrockConnection:
		draft.Bedrock = &routing.BedrockConnectionDraft{Region: connection.Region().String(), Endpoint: connection.Endpoint(), Credential: connection.Credential().String()}
	case routing.CustomConnection:
		draft.Custom = &routing.CustomConnectionDraft{BaseURL: connection.BaseURL().String()}
		if auth := connection.Auth(); auth != nil {
			header, ok := auth.(routing.CustomHeaderAuth)
			if !ok {
				return routing.ConnectionDraft{}, fmt.Errorf("unsupported custom auth %T", auth)
			}
			draft.Custom.Header = &routing.CustomHeaderDraft{Name: header.Name(), Credential: header.Credential().String()}
		}
	default:
		return routing.ConnectionDraft{}, fmt.Errorf("unsupported routing connection %T", connection)
	}
	return draft, nil
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
